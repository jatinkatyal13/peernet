package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/pion/stun/v3"
)

var server = flag.String("server", "stun1.l.google.com:19302", "Stun server address") //nolint:gochecknoglobals

const (
	udp           = "udp4"
	pingMsg       = "ping"
	pongMsg       = "pong"
	keepaliveMsg  = "keepalive"
	timeoutMillis = 500
)

func main() { //nolint:gocognit
	flag.Parse()

	srvAddr, err := net.ResolveUDPAddr(udp, *server)
	if err != nil {
		log.Fatalf("Failed to resolve server addr: %s", err)
	}

	conn, err := net.ListenUDP(udp, nil)
	if err != nil {
		log.Fatalf("Failed to listen: %s", err)
	}

	defer func() {
		_ = conn.Close()
	}()

	log.Printf("Listening on %s", conn.LocalAddr())

	var publicAddr stun.XORMappedAddress
	var peerAddr *net.UDPAddr

	messageChan := listen(conn)
	var inputChan <-chan string
	var peerAddrChan <-chan string

	keepalive := time.Tick(timeoutMillis * time.Millisecond)

	for {
		select {
		case message, ok := <-messageChan:
			if !ok {
				return
			}

			switch {
			case string(message) == keepaliveMsg:
				continue
			case stun.IsMessage(message):
				m := new(stun.Message)
				m.Raw = message
				decErr := m.Decode()
				if decErr != nil {
					log.Println("decode:", decErr)
					break
				}
				var xorAddr stun.XORMappedAddress
				if getErr := xorAddr.GetFrom(m); getErr != nil {
					log.Println("getFrom:", getErr)
					break
				}

				if publicAddr.String() != xorAddr.String() {
					log.Printf("My public address: %s\n", xorAddr)
					publicAddr = xorAddr

					peerAddrChan = getPeerAddr()
				}

			default:
				log.Println("Message: ", string(message))
			}

		case peerStr := <-peerAddrChan:
			peerAddr, err = net.ResolveUDPAddr(udp, peerStr)
			if err != nil {
				log.Panicln("resolve peeraddr:", err)
			}
			inputChan = inputStream()

		case input := <-inputChan:
			if peerAddr == nil {
				log.Println("Peer address is not known yet")
			} else {
				err = sendStr(input, conn, peerAddr)
				if err != nil {
					log.Panicln("send:", err)
				}
			}

		case <-keepalive:
			// Keep NAT binding alive using STUN server or the peer once it's known
			if peerAddr == nil {
				err = sendBindingRequest(conn, srvAddr)
			} else {
				err = sendStr(keepaliveMsg, conn, peerAddr)
			}

			if err != nil {
				log.Panicln("keepalive:", err)
			}
		}
	}
}

func getPeerAddr() <-chan string {
	result := make(chan string)

	go func() {
		reader := bufio.NewReader(os.Stdin)
		log.Println("Enter remote peer address:")
		peer, _ := reader.ReadString('\n')
		result <- strings.Trim(peer, " \r\n")
	}()

	return result
}

func inputStream() <-chan string {
	result := make(chan string)

	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			text, _ := reader.ReadString('\n')
			result <- strings.Trim(text, " \r\n")
		}
	}()

	return result
}

func listen(conn *net.UDPConn) <-chan []byte {
	messages := make(chan []byte)
	go func() {
		for {
			buf := make([]byte, 1024)

			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				close(messages)
				return
			}
			buf = buf[:n]

			messages <- buf
		}
	}()
	return messages
}

func sendBindingRequest(conn *net.UDPConn, addr *net.UDPAddr) error {
	m := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	err := send(m.Raw, conn, addr)
	if err != nil {
		return fmt.Errorf("binding: %w", err)
	}

	return nil
}

func send(msg []byte, conn *net.UDPConn, addr *net.UDPAddr) error {
	_, err := conn.WriteToUDP(msg, addr)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}

	return nil
}

func sendStr(msg string, conn *net.UDPConn, addr *net.UDPAddr) error {
	return send([]byte(msg), conn, addr)
}
