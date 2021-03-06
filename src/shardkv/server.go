package shardkv

import "net"
import "fmt"
import "net/rpc"
import "log"
import "time"
import "paxos"
import "sync"
import "sync/atomic"
import "os"
import "syscall"
import "encoding/gob"
import "math/rand"
import "shardmaster"

//Lab4_PartB
import "strconv"


const Debug = 0

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug > 0 {
		log.Printf(format, a...)
	}
	return
}


type Op struct {
	// Your definitions here.
	//Lab4_PartB
	Key       string   // key
	Value     string   // value
	Op        string   // "Put", "Append" or "Get"
	Me        string   // the id of the client
	Ts        string   // the timestamp of a operation
	Index     int      // the index of the config
	Database  map[string]string
	Config    shardmaster.Config
	Logstime  map[string]string
}


type ShardKV struct {
	mu         sync.Mutex
	l          net.Listener
	me         int
	dead       int32 // for testing
	unreliable int32 // for testing
	sm         *shardmaster.Clerk
	px         *paxos.Paxos

	gid int64 // my replica group ID

	// Your definitions here.
	//Lab4_PartB
	database   map[string]string   //database
	logstime   map[string]string   //operation logs
	config     shardmaster.Config  //config
	index      int                 //index of the config
	seq        int                 //max seq numvber
	Me         string              //client id ,for reconfig
}


func (kv *ShardKV) Get(args *GetArgs, reply *GetReply) error {
	// Your code here.
	//Lab4_PartB
	//kv.index = kv.config.Num
	if (args.Index > kv.config.Num) {
		reply.Err = ErrIndex
		return nil
	}
	kv.mu.Lock()
	defer kv.mu.Unlock()
	proposal := Op{
		args.Key, 
		"", 
		args.Op, 
		args.Me, 
		args.Ts, 
		args.Index, 
		map[string]string{}, 
		shardmaster.Config{},
		map[string]string{}}
	kv.UpdateDB(proposal)
	shard := key2shard(args.Key)
	if (kv.config.Shards[shard] != kv.gid) {
		reply.Err = ErrWrongGroup
		return nil
	}
	//fmt.Println(kv.config,"\n",kv.database,"\n\n")
	val, exist := kv.database[args.Key]
	if (exist == false) {
		reply.Err = ErrNoKey
	} else {
		reply.Err = OK
		reply.Value = val
	}
	return nil
}

// RPC handler for client Put and Append requests
func (kv *ShardKV) PutAppend(args *PutAppendArgs, reply *PutAppendReply) error {
	// Your code here.
	//Lab4_PartB
	//kv.index = kv.config.Num
	if (args.Index > kv.config.Num) {
		reply.Err = ErrIndex
		return nil
	}
	
	kv.mu.Lock()
	defer kv.mu.Unlock()
	proposal := Op{
		args.Key, 
		args.Value,
		args.Op, 
		args.Me, 
		args.Ts, 
		args.Index, 
		map[string]string{}, 
		shardmaster.Config{}, 
		map[string]string{}}
	kv.UpdateDB(proposal)
	shard := key2shard(args.Key)
	if (kv.config.Shards[shard] != kv.gid) {
		reply.Err = ErrWrongGroup
		return nil
	}
	reply.Err = OK
	return nil
}

//Lab4_PartB
func (kv *ShardKV) UpdateDB(op Op) {
	for {
		//fmt.Println(op)
		if (op.Op == "Reconfig") {
			if (op.Config.Num <= kv.config.Num) {
				return
			}
		} else if (op.Op == "GetData") {
		} else {
			ts, exist := kv.logstime[op.Me + op.Op]
			if (exist && ts >= op.Ts) {
				return
			}
			shard := key2shard(op.Key)
			if (kv.config.Shards[shard] != kv.gid) {
				return
			}
		}
		kv.seq++
		kv.px.Start(kv.seq, op)
		Act := Op{}
		to := 10 * time.Millisecond
		for {
			stat, act := kv.px.Status(kv.seq)
			if (stat == paxos.Decided) {
				Act = act.(Op)
				break
			}
			time.Sleep(to)
			if (to < 10*time.Second) {
				to *= 2
			}
		}
		kv.ProcOperation(Act)
		kv.px.Done(kv.seq)
		if (op.Ts == Act.Ts) {
			return
		}
	}
}

//Lab4_PartB
func (kv *ShardKV) ProcOperation(op Op) {
	// ts, exist := kv.database[op.Me + op.Op]
	// if (exist && ts >= op.Ts) {
	// 	return
	// }
	// shard := key2shard(op.Key)
	// if (kv.config.Shards[shard] != kv.gid) {
	// 	return
	// }
	if (op.Op == "GetData") {
		return
	}
	if (op.Op == "Put") {
		kv.database[op.Key] = op.Value
		kv.logstime[op.Me + op.Op] = op.Ts
	} else if (op.Op == "Append") {
		kv.database[op.Key] += op.Value
		kv.logstime[op.Me + op.Op] = op.Ts
	} else if (op.Op == "Reconfig") {
		//fmt.Println("SSSSSSSSSSSSSSSSSSSSSSS")
		for k, v := range op.Database {
			kv.database[k] = v
			
		}
		for k, v := range op.Logstime {
			val, exs := kv.logstime[k]
			if !(exs && val >= v) {
				kv.logstime[k] = v
			}
		}
		// fmt.Println(op.Config.Num)
		//fmt.Println(kv.config)
		kv.config = op.Config
		//fmt.Println(kv.config)
		// kv.index = kv.config.Num

	}
	return
}

//Lab4_PartB
func (kv *ShardKV) GetShardDatabase(args *GetShardDatabaseArgs, reply *GetShardDatabaseReply) error {
	if (args.Index > kv.config.Num) {
		reply.Err = ErrIndex
		return nil
	}
	kv.mu.Lock()
	defer kv.mu.Unlock()
	ts := strconv.FormatInt(time.Now().UnixNano(), 10)
	proposal := Op{Op: "GetData", Ts:ts}
	kv.UpdateDB(proposal)
	if (args.Index > kv.config.Num) {
		reply.Err = ErrIndex
		return nil
	}
	shard := args.Shard

	dbs := map[string]string{}
	lgs := map[string]string{}
	for k, v := range kv.database {
		if (key2shard(k) == shard) {
			dbs[k] = v
		}
	}
	for k, v := range kv.logstime {
		lgs[k] = v
	}
	 
	reply.Err = OK
	reply.Database = dbs
	reply.Logstime = lgs
	return nil
}

//
// Ask the shardmaster if there's a new configuration;
// if so, re-configure.
//
func (kv *ShardKV) tick() {
	//Lab4_PartB
	kv.mu.Lock()
	defer kv.mu.Unlock()
	config := kv.sm.Query(-1)
	if (kv.config.Num == -1 && config.Num == 1) {
		kv.config = config
		return
	}
	for ind := kv.config.Num+1; ind <= config.Num; ind++ {
		cfg := kv.sm.Query(ind)
		database_newpart := map[string]string{}
		logstime_newpart := map[string]string{}
		for shard, gid_old := range kv.config.Shards {
			gid_new := cfg.Shards[shard]
			if (gid_new != gid_old && gid_new == kv.gid) {
				label := false
				for _, srv := range kv.config.Groups[gid_old] {
					args := &GetShardDatabaseArgs{shard, kv.config.Num, kv.database, kv.Me}
					reply := GetShardDatabaseReply{OK, map[string]string{}, map[string]string{}}		
	 				ok := call(srv, "ShardKV.GetShardDatabase", args, &reply)
	 				if (ok && reply.Err == OK) {
	 					for k, v := range reply.Database {
	 						database_newpart[k] = v 
	 					}
	 					for k, v := range reply.Logstime {
	 						val, exist := logstime_newpart[k]
	 						if !(exist && val >= v) {
								logstime_newpart[k] = v
							}
	 					}
	 					label = true
	 				}
				}
				if (label == false && gid_old > 0) {
					return
				}
			}
		}
		ts := strconv.FormatInt(time.Now().UnixNano(), 10)
		proposal := Op{"", "", "Reconfig", kv.Me, ts, ind, database_newpart, cfg, logstime_newpart}
		kv.UpdateDB(proposal)
	}
}

// tell the server to shut itself down.
// please don't change these two functions.
func (kv *ShardKV) kill() {
	atomic.StoreInt32(&kv.dead, 1)
	kv.l.Close()
	kv.px.Kill()
}

func (kv *ShardKV) isdead() bool {
	return atomic.LoadInt32(&kv.dead) != 0
}

// please do not change these two functions.
func (kv *ShardKV) Setunreliable(what bool) {
	if what {
		atomic.StoreInt32(&kv.unreliable, 1)
	} else {
		atomic.StoreInt32(&kv.unreliable, 0)
	}
}

func (kv *ShardKV) isunreliable() bool {
	return atomic.LoadInt32(&kv.unreliable) != 0
}

//
// Start a shardkv server.
// gid is the ID of the server's replica group.
// shardmasters[] contains the ports of the
//   servers that implement the shardmaster.
// servers[] contains the ports of the servers
//   in this replica group.
// Me is the index of this server in servers[].
//
func StartServer(gid int64, shardmasters []string,
	servers []string, me int) *ShardKV {
	gob.Register(Op{})

	kv := new(ShardKV)
	kv.me = me
	kv.gid = gid
	kv.sm = shardmaster.MakeClerk(shardmasters)

	// Your initialization code here.
	// Don't call Join().
	//Lab4_PartB
	kv.database = map[string]string{}
	kv.config = shardmaster.Config{Num:-1}
	kv.logstime = map[string]string{}
	//kv.index = 0
	kv.seq = 0
	//kv.Me = strconv.FormatInt(nrand(), 10)


	rpcs := rpc.NewServer()
	rpcs.Register(kv)

	kv.px = paxos.Make(servers, me, rpcs)


	os.Remove(servers[me])
	l, e := net.Listen("unix", servers[me])
	if e != nil {
		log.Fatal("listen error: ", e)
	}
	kv.l = l

	// please do not change any of the following code,
	// or do anything to subvert it.

	go func() {
		for kv.isdead() == false {
			conn, err := kv.l.Accept()
			if err == nil && kv.isdead() == false {
				if kv.isunreliable() && (rand.Int63()%1000) < 100 {
					// discard the request.
					conn.Close()
				} else if kv.isunreliable() && (rand.Int63()%1000) < 200 {
					// process the request but force discard of reply.
					c1 := conn.(*net.UnixConn)
					f, _ := c1.File()
					err := syscall.Shutdown(int(f.Fd()), syscall.SHUT_WR)
					if err != nil {
						fmt.Printf("shutdown: %v\n", err)
					}
					go rpcs.ServeConn(conn)
				} else {
					go rpcs.ServeConn(conn)
				}
			} else if err == nil {
				conn.Close()
			}
			if err != nil && kv.isdead() == false {
				fmt.Printf("ShardKV(%v) accept: %v\n", me, err.Error())
				kv.kill()
			}
		}
	}()

	go func() {
		for kv.isdead() == false {
			kv.tick()
			time.Sleep(250 * time.Millisecond)
		}
	}()

	return kv
}
