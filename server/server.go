package server

import (
	"github.com/ha/doozerd/consensus"
	"github.com/ha/doozerd/store"
	"log"
	"net"
	"syscall"
	"strings"
)

// ListenAndServe listens on l, accepts network connections, and
// handles requests according to the doozer protocol.
func ListenAndServe(l net.Listener, canWrite chan bool, st *store.Store, p consensus.Proposer, rwsk, rosk string, self string) {
	var w bool
	for {
		c, err := l.Accept()
		if err != nil {
			if err == syscall.EINVAL {
				break
			}
			if e, ok := err.(*net.OpError); ok && e.Err == syscall.EINVAL {
				break
			}
			log.Println(err)
			continue
		}

		// has this server become writable?
		select {
		case w = <-canWrite:
			canWrite = nil
		default:
		}

		go serve(c, st, p, w, rwsk, rosk, self)
	}
}

func serve(nc net.Conn, st *store.Store, p consensus.Proposer, w bool, rwsk, rosk string, self string) {
	client_addr := strings.Replace(nc.RemoteAddr().String(), ":", "-", 1)
	eph_node := "/ctl/node/"+self+"/client/" + client_addr

	c := &conn{
		c:        nc,
		addr:     nc.RemoteAddr().String(),
		eph_node: eph_node,
		st:       st,
		p:        p,
		canWrite: w,
		rwsk:     rwsk,
		rosk:     rosk,
		self:     self,
	}

	// create the ephemeral node on client connect under this
	// node's tree. the list of all clients connected can be
	// determined by globbing /ctl/node/*/client/*. to set
	// arbitrary value in the ephemeral node for that client,
	// simply call SET('/eph', value), where /eph is a symlink to
	// that client's ephemeral node.
	log.Println("** setting ephemeral node", eph_node)
	ev := consensus.Set(p, eph_node, []byte("ACTIVE"), store.Missing)
	if ev.Err != nil {
		log.Println("** failed to set ephemeral node:", ev.Err)
		nc.Close()
		return
	}


	c.grant("") // start as if the client supplied a blank password
	c.serve()

	// delete the ephemeral node on disconnect
	log.Println("** deleting ephemeral node", eph_node)
	ev = consensus.Del(p, eph_node, store.Clobber)
	if ev.Err != nil {
		log.Println("** failed to delete ephemeral node:", ev.Err)
	}

	nc.Close()

}
