package loadb

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
)

type Loadbalancer struct {
	Port            string
	Servers         []Server
	RoundRobinCount int
	SessionTable    map[string]Server
	mu              sync.Mutex
	Algorithm       string // "roundrobin" or "leastconn"
}

func NewLoadbalancer(p string, Servers []Server, algorithm string) *Loadbalancer {
	return &Loadbalancer{
		Port:            p,
		RoundRobinCount: 0,
		Servers:         Servers,
		SessionTable:    make(map[string]Server),
		Algorithm:       algorithm,
	}
}

func (lb *Loadbalancer) GetNextAvailableServer() Server {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	server := lb.Servers[lb.RoundRobinCount%len(lb.Servers)]
	for !server.IsAlive() {
		lb.RoundRobinCount++
		server = lb.Servers[lb.RoundRobinCount%len(lb.Servers)]
	}
	lb.RoundRobinCount++
	return server
}

func (lb *Loadbalancer) GetLeastConnServer() Server {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	var selected Server
	for _, server := range lb.Servers {
		if !server.IsAlive() {
			continue
		}
		if selected == nil || server.ActiveConn() < selected.ActiveConn() {
			selected = server
		}
	}
	return selected
}

// Sticky Http ie: Session based Server allocation Works!
func (lb *Loadbalancer) ServeProxy(wr http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("Session-ID")
	var targetServer Server

	if sessionID != "" {
		if lb.SessionTable[sessionID] != nil {
			targetServer = lb.SessionTable[sessionID]
		} else {
			targetServer = lb.GetNextAvailableServer()
			lb.SessionTable[sessionID] = targetServer
		}
	} else {
		if lb.Algorithm == "lc" {
			targetServer = lb.GetLeastConnServer()
		} else if lb.Algorithm == "rr" {
			targetServer = lb.GetNextAvailableServer()
		}
	}
	targetServer.IncActiveConn()

	log.Printf("forwarding requests to address %q\n", targetServer.Address())
	targetServer.Serve(wr, r)

	// lb.mu.Lock()
	// targetServer.DecActiveConn()
	// lb.mu.Unlock()
}

type Server interface {
	Address() string
	IsAlive() bool
	Serve(rw http.ResponseWriter, r *http.Request)
	ActiveConn() int
	IncActiveConn()
	DecActiveConn()
}

type SimpleServer struct {
	addr       string
	proxy      httputil.ReverseProxy
	activeConn int
	mu         sync.Mutex
}

func NewServer(addr string) *SimpleServer {
	serverUrl, err := url.Parse(addr)
	if err != nil {
		panic(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(serverUrl)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		for key, values := range req.Header {
			req.Header[key] = values
		}
		req.Host = serverUrl.Host
	}
	return &SimpleServer{
		addr:  addr,
		proxy: *proxy,
	}
}

func (s *SimpleServer) Address() string {
	return s.addr
}

func (s *SimpleServer) IsAlive() bool {
	return true
}

func (s *SimpleServer) Serve(w http.ResponseWriter, r *http.Request) {
	s.proxy.ServeHTTP(w, r)
}

func (s *SimpleServer) ActiveConn() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeConn
}

func (s *SimpleServer) IncActiveConn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeConn++
}

func (s *SimpleServer) DecActiveConn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeConn--
}
