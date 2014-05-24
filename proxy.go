package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"regexp"
)

// Read from "conn" and send data to "out", read from "in" and write to "conn".
func proxy(conn net.Conn, in chan []byte, out chan []byte) {
	// Read routine, closes "out" on read error (like EOF).
	go func() {
		for {
			buf := make([]byte, 2048)
			numBytes, err := conn.Read(buf)
			if err != nil {
				fmt.Printf("Read error: %v\n", err)
				close(out)
				conn.Close()
				break
			}
			fmt.Printf("Sent %d bytes to channel\n", numBytes)
			out <- buf[:numBytes]
		}
	}()

	// Write routine (read from "in", write to "conn").
	go func() {
		for {
			buf, ok := <-in
			if !ok {
				fmt.Println("Channel got closed")
				conn.Close()
				break
			}
			fmt.Printf("Got %d bytes from channel\n", len(buf))
			_, err := conn.Write(buf)
			if err != nil {
				fmt.Printf("Write error: %v\n", err)
				conn.Close()
				break
			}
		}
	}()
}

func getTargetHttp(conn net.Conn, out chan []byte) (string, []byte, error) {
	reHost := regexp.MustCompile("(?m)^Host: (.+)\r\n")
	reHeaderEnd := regexp.MustCompile("\r\n\r\n")
	buf := make([]byte, 8192)
	pos := 0
	for {
		// Read new bytes from connection
		tmpBuf := make([]byte, len(buf)-pos)
		numBytes, err := conn.Read(tmpBuf)
		if err != nil {
			return "", nil, errors.New(fmt.Sprintf("Read error (%v)", err))
		}

		fmt.Printf("pos: %v, numBytes: %v\n", pos, numBytes)
		// Store new data in "buf"
		copy(buf[pos:], tmpBuf[:numBytes])
		pos += numBytes
		fmt.Printf("Current data:\n%v\n", string(buf[:pos]))

		// Try to find the "Host: " header in "buf"
		if matches := reHost.FindSubmatch(buf); len(matches) > 0 {
			return string(matches[1]), buf[:pos], nil
		}

		// Check for an empty line (indicates end of header)
		if matches := reHeaderEnd.FindSubmatch(buf); len(matches) > 0 {
			return "", nil, errors.New("No Host header found!")
		}

		// Check if we hit our buffer limit
		if len(buf) == pos {
			return "", nil, errors.New("Buffer limit exceeded!")
		}
	}
}

func handleConnHttp(cConn net.Conn) {
	fmt.Printf("Handling new connection from %v\n", cConn.RemoteAddr())

	clientToServer := make(chan []byte)
	serverToClient := make(chan []byte)

	target, readBuf, err := getTargetHttp(cConn, clientToServer)
	if err != nil {
		fmt.Printf("Error getting target: %v\n", err)
		cConn.Close()
		return
	}

	fmt.Printf("Connecting to %s:http\n", target)
	sConn, err := net.Dial("tcp", fmt.Sprintf("%s:http", target))
	if err != nil {
		fmt.Printf("Error connecting to server: %v\n", err)
		cConn.Close()
		return
	}

	fmt.Println("Starting proxy")

	go proxy(sConn, clientToServer, serverToClient)
	clientToServer <- readBuf
	go proxy(cConn, serverToClient, clientToServer)
}

func startProxyHttp() {
	fmt.Println("Starting up http listener")
	listener, err := net.Listen("tcp", ":http")
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	fmt.Println("Waiting for connections")
	for {
		conn, err := listener.Accept()
		if err == nil {
			go handleConnHttp(conn)
		} else {
			fmt.Println("Client error: ", err)
		}
	}
}

func main() {
	startProxyHttp()
}
