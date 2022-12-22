package vncproxy

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

type TokenHandler func(r *http.Request) (addr, mode string, err error)

// Config represents vnc proxy config
type Config struct {
	LogLevel    uint32
	Logger      Logger
	DialTimeout time.Duration
	TokenHandler
}

// Proxy represents vnc proxy
type Proxy struct {
	logLevel     uint32
	logger       *logger
	dialTimeout  time.Duration // Timeout for connecting to each target vnc server
	peers        map[*peer]struct{}
	l            sync.RWMutex
	tokenHandler TokenHandler
}

// New returns a vnc proxy
// If token handler is nil, vnc backend address will always be :5901
func New(conf *Config) *Proxy {
	if conf.TokenHandler == nil {
		conf.TokenHandler = func(r *http.Request) (addr, mode string, err error) {
			return ":5901", "tcp", nil
		}
	}

	return &Proxy{
		logLevel:     conf.LogLevel,
		logger:       NewLogger(conf.LogLevel, conf.Logger),
		dialTimeout:  conf.DialTimeout,
		peers:        make(map[*peer]struct{}),
		l:            sync.RWMutex{},
		tokenHandler: conf.TokenHandler,
	}
}

// ServeWS provides websocket handler
func (p *Proxy) ServeWS(ws *websocket.Conn) {
	p.logger.Debugf("ServeWS")
	ws.PayloadType = websocket.BinaryFrame

	r := ws.Request()
	p.logger.Debugf("request url: %v", r.URL)

	// get vnc backend server addr
	addr, mode, err := p.tokenHandler(r)
	if err != nil {
		p.logger.Infof("get vnc backend failed: %v", err)
		return
	}

	peer, err := NewPeer(ws, mode, addr, p.dialTimeout)
	if err != nil {
		p.logger.Infof("new vnc peer failed: %v", err)
		return
	}

	p.addPeer(peer)
	defer func() {
		p.logger.Info("close peer")
		p.deletePeer(peer)
	}()

	go func() {
		if err := peer.ReadTarget(); err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			p.logger.Info(err)
			return
		}
	}()

	if err = peer.ReadSource(); err != nil {
		if strings.Contains(err.Error(), "use of closed network connection") {
			return
		}
		p.logger.Info(err)
		return
	}
}

func (p *Proxy) addPeer(peer *peer) {
	p.l.Lock()
	p.peers[peer] = struct{}{}
	p.l.Unlock()
}

func (p *Proxy) deletePeer(peer *peer) {
	p.l.Lock()
	delete(p.peers, peer)
	peer.Close()
	p.l.Unlock()
}

func (p *Proxy) Peers() map[*peer]struct{} {
	p.l.RLock()
	defer p.l.RUnlock()
	return p.peers
}
