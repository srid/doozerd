package consensus


import (
	"container/heap"
	"container/vector"
	"doozer/store"
	"goprotobuf.googlecode.com/hg/proto"
	"log"
	"time"
)


type packet struct {
	Addr string
	M
}


func (p packet) Less(y interface{}) bool {
	return *p.Seqn < *y.(packet).Seqn
}


type Packet struct {
	Addr string
	Data []byte
}


type fill struct {
	t int64 // trigger time
	n int64 // seqn
}


func (f fill) Less(y interface{}) bool {
	return f.n < y.(fill).n
}


type Stats struct {
	Runs        int
	WaitPackets int
	WaitFills   int
	Running     int
}


type Manager <-chan Stats


type Prop struct {
	Seqn int64
	Mut  []byte
}


func newManager(self string, nextFill int64, propSeqns chan<- int64, in <-chan Packet, runs <-chan *run, props <-chan *Prop, ticker <-chan int64, fillDelay int64, st *store.Store, out chan<- Packet) Manager {
	statCh := make(chan Stats)

	go func() {
		running := make(map[int64]*run)
		packets := new(vector.Vector)
		fills := new(vector.Vector)
		ticks := make(chan int64)
		var nextRun int64
		var stats Stats

		for {
			stats.Runs = len(running)
			stats.WaitPackets = packets.Len()
			stats.WaitFills = fills.Len()
			stats.Running = len(running)

			select {
			case run := <-runs:
				if closed(runs) {
					return
				}

				running[run.seqn] = run
				nextRun = run.seqn + 1
				run.ticks = ticks
				if run.isLeader(self) {
					propSeqns <- run.seqn
				}
			case p := <-in:
				recvPacket(packets, p)
			case n := <-ticks:
				applyTick(packets, n)
			case statCh <- stats:
			case pr := <-props:
				log.Printf("propose seqn=%d", pr.Seqn)
				m := M{Seqn: &pr.Seqn, Cmd: propose, Value: pr.Mut}
				heap.Push(packets, packet{M: m})

				for nextFill < pr.Seqn {
					schedFill(fills, nextFill, fillDelay)
					nextFill++
				}
				nextFill++
			case t := <-ticker:
				for fills.Len() > 0 {
					f := fills.At(0).(fill)

					if f.n >= t {
						break
					}

					heap.Pop(fills)

					m := M{Seqn: &f.n, Cmd: propose, Value: []byte(store.Nop)}
					heap.Push(packets, packet{M: m})
				}
			}

			for packets.Len() > 0 {
				p := packets.At(0).(packet)

				seqn := *p.Seqn

				if seqn >= nextRun {
					break
				}

				heap.Pop(packets)

				r := running[seqn]
				if r == nil {
					go sendLearn(out, p, st)
					continue
				}

				learned := r.deliver(p)
				if learned {
					running[seqn] = nil, false
				}
			}
		}
	}()

	return statCh
}


func sendLearn(out chan<- Packet, p packet, st *store.Store) {
	if p.M.Cmd != nil && *p.M.Cmd == M_INVITE {
		e := <-st.Wait(*p.Seqn)

		if e.Err != nil {
			log.Println(e.Err)
		} else {
			m := M{
				Seqn:  &e.Seqn,
				Cmd:   learn,
				Value: []byte(e.Mut),
			}
			buf, _ := proto.Marshal(&m)
			out <- Packet{p.Addr, buf}
		}
	}
}


func recvPacket(q heap.Interface, P Packet) {
	var p packet
	p.Addr = P.Addr

	err := proto.Unmarshal(P.Data, &p.M)
	if err != nil {
		return
	}

	if p.M.Seqn == nil {
		return
	}

	heap.Push(q, p)
}


func schedFill(q heap.Interface, n, fillDelay int64) {
	heap.Push(q, fill{n: n, t: time.Nanoseconds() + fillDelay})
}


func applyTick(q heap.Interface, n int64) {
	heap.Push(q, packet{M: M{Seqn: proto.Int64(n), Cmd: tick}})
}
