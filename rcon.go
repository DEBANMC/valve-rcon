package rcon

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// RCON implementation based on: https://developer.valvesoftware.com/wiki/Source_RCON_Protocol

const (
	DefaultPort = 27015
)

type RCON struct {
	host           string
	port           int
	password       string
	commandHandler func(command string, client Client)
	banList        []string
	listener       net.Listener
}

func NewRCON(host string, port int, password string) *RCON {
	return &RCON{
		host:     host,
		port:     port,
		password: password,
	}
}

func (r *RCON) SetBanList(banList []string) {
	r.banList = banList
}

func (r *RCON) OnCommand(handle func(command string, client Client)) {
	r.commandHandler = handle
}

func (r *RCON) addressInBanList(addr string) bool {
	for _, a := range r.banList {
		if a == addr {
			return true
		}
	}

	return false
}

func (r *RCON) ListenAndServe() error {
	l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", r.host, r.port))
	if err != nil {
		return err
	}
	r.listener = l
	defer r.listener.Close()

	for {

		conn, err := r.listener.Accept()
		if err != nil {
			break
		}

		ip := addressWithoutPort(conn.RemoteAddr().String())

		if r.addressInBanList(ip) {
			_ = conn.Close()
			continue
		}

		go r.acceptConnection(conn)
	}

	return nil
}

func (r *RCON) acceptConnection(conn net.Conn) {
	authenticated := false

	for {

		p, err := ParsePacket(conn)
		if errors.Is(err, io.EOF) {
			_ = conn.Close()
			break
		}

		if err != nil {
			continue
		}

		// handle commands
		if authenticated && p.Type == ServerDataExecCommand {
			if r.commandHandler != nil {
				r.commandHandler(p.Body, NewClient(conn, p))
			}
			continue
		}

		// not authenticated and not a ServerDataAuth packet
		if p.Type != ServerDataAuth {
			_ = conn.Close()
			break
		}

		// empty password, we should refuse the connection
		if p.Type == ServerDataAuth && r.password == "" {
			_ = conn.Close()
			break
		}

		// authentication
		if p.Type == ServerDataAuth {
			correct := p.Body == r.password
			id := int32(-1)

			if correct {
				id = p.ID
			}

			responsePacket := Packet{ID: id, Type: ServerDataAuthResponse}
			responseBytes, _ := EncodePacket(responsePacket)
			_, _ = conn.Write(responseBytes)

			if correct {
				authenticated = true
			} else {
				_ = conn.Close()
				break
			}

			continue
		}
	}
}

func addressWithoutPort(addr string) string {
	parts := strings.Split(addr, ":")
	return parts[0]
}

func (server *RCON) CloseOnProgramEnd() {
	c := make(chan os.Signal, 2)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-c
		server.listener.Close()
	}()
}
