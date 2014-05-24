package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"regexp"
)

type HttpProxyConnection struct {
	// Server hostname
	server string

	// Client and Server connection
	cConn, sConn net.Conn

	// Buffer for initial read
	headBuf []byte
}

type HttpsProxyConnection struct {
	// Server hostname
	server string

	// Client and Server connection
	cConn, sConn net.Conn

	// Buffer for initial read
	tlsBuf []byte
}

func PipeConn(in net.Conn, out net.Conn) {
	// Make sure to close both connections in the end
	defer in.Close()
	defer out.Close()

	buf := make([]byte, 2048)

	for {
		numBytes, err := in.Read(buf)
		if err != nil {
			fmt.Printf("Read error: %v\n", err)
			break
		}
		_, err = out.Write(buf[:numBytes])
		if err != nil {
			fmt.Printf("Write error: %v\n", err)
			break
		}
	}
}

func (http *HttpProxyConnection) ParseHeader() error {
	// Regexp to find the Host line and to check for an empty line
	reHost := regexp.MustCompile("(?m)^Host: (.+)\r\n")
	reHeaderEnd := regexp.MustCompile("\r\n\r\n")

	// Buffer to hold the data we read, use 8k as maximum header size
	buf := make([]byte, 8192)
	pos := 0

	for {
		// Read new bytes from connection
		tmpBuf := make([]byte, len(buf)-pos)
		numBytes, err := http.cConn.Read(tmpBuf)
		if err != nil {
			return errors.New(fmt.Sprintf("Read error (%v)", err))
		}

		// Store new data in "buf"
		copy(buf[pos:], tmpBuf[:numBytes])
		pos += numBytes
		//fmt.Printf("Current data:\n%v\n", string(buf[:pos]))

		// Try to find the "Host: " header in "buf"
		if matches := reHost.FindSubmatch(buf); len(matches) > 0 {
			http.server = string(matches[1])
			http.headBuf = buf[:pos]
			return nil
		}

		// Check for an empty line (indicates end of header)
		if matches := reHeaderEnd.FindSubmatch(buf); len(matches) > 0 {
			return errors.New("No Host header found!")
		}

		// Check if we hit our buffer limit
		if len(buf) == pos {
			return errors.New("Buffer limit exceeded!")
		}
	}
}

func (http *HttpProxyConnection) HandleConn() {
	fmt.Printf("Handling new connection from %v\n", http.cConn.RemoteAddr())
	var err error

	// Extract desired server from HTTP header
	if err = http.ParseHeader(); err != nil {
		fmt.Printf("Error getting server: %v\n", err)
		http.cConn.Close()
		return
	}

	// TODO(fabi): Figure out what to do if server contains a port number

	// Create connection to server
	fmt.Printf("Connecting to %s:http\n", http.server)
	http.sConn, err = net.Dial("tcp", fmt.Sprintf("%s:http", http.server))
	if err != nil {
		fmt.Printf("Error connecting to server: %v\n", err)
		http.cConn.Close()
		http.sConn.Close()
		return
	}

	// Send data we read during parsing to server
	if _, err = http.sConn.Write(http.headBuf); err != nil {
		fmt.Printf("Write error: %v\n", err)
		http.cConn.Close()
		http.sConn.Close()
		return
	}

	// Link both connections together
	fmt.Println("Linking client and server connection")
	go PipeConn(http.cConn, http.sConn)
	go PipeConn(http.sConn, http.cConn)
}

func (https *HttpsProxyConnection) ParseTlsHandshake() error {
	// Buffer to hold the data we read, use 8k as maximum size
	buf := make([]byte, 8192)
	pos := 0

	for {
		// Read new bytes from connection
		tmpBuf := make([]byte, len(buf)-pos)
		numBytes, err := https.cConn.Read(tmpBuf)
		if err != nil {
			return errors.New(fmt.Sprintf("Read error (%v)", err))
		}

		// Store new data in "buf"
		copy(buf[pos:], tmpBuf[:numBytes])
		pos += numBytes

		// We need the header to start parsing
		if pos < 5 {
			continue
		}

		// Check content type
		//fmt.Printf("TLS content type: %d\n", buf[0])
		if buf[0] != 0x16 {
			return errors.New("Data doesn't look like a TLS handshake!")
		}

		// We need at least SSL 3
		//fmt.Printf("SSL version: %d.%d\n", buf[1], buf[2])
		if buf[1] < 3 {
			return errors.New(fmt.Sprintf("Incompatible SSL Version (%d.%d)!", buf[1], buf[2]))
		}

		// Make sure we have the whole handshake
		len := int((buf[3] << 8) + buf[4] + 5)
		//fmt.Printf("need: %d, have:%d\n", len, pos)
		if pos < len {
			continue
		}

		readPos := 5

		// Check handshake type
		if readPos+1 > pos {
			return errors.New("Not enough data!")
		}
		if buf[5] != 0x01 {
			return errors.New("Not a handshake client hello!")
		}

		// Skip Handshake Type, Length, Version, Random
		readPos += 38

		// Skip Session ID
		if readPos+1 > pos {
			return errors.New("Not enough data!")
		}
		readPos += 1 + int(buf[readPos])

		// Skip Cipher Suites
		if readPos+2 > pos {
			return errors.New("Not enough data!")
		}
		readPos += 2 + int((buf[readPos]<<8)+buf[readPos+1])

		// Skip Compression Methods
		if readPos+1 > pos {
			return errors.New("Not enough data!")
		}
		readPos += 1 + int(buf[readPos])

		// Check for extensions
		if pos == readPos {
			return errors.New("No TLS extensions!")
		}

		// Get and skip Extensions Length
		if readPos+2 > pos {
			return errors.New("Not enough data!")
		}
		extLen := int((buf[readPos] << 8) + buf[readPos+1])
		readPos += 2

		// Loop through extensions
		for extPos := 0; extPos+4 <= extLen; {
			extReadPos := readPos + extPos
			len := int((buf[extReadPos+2] << 8) + buf[extReadPos+3])

			// Is it a server name extension?
			if buf[extReadPos] == 0x00 && buf[extReadPos+1] == 0x00 {
				if extReadPos+4+len > pos {
					return errors.New("Not enough data!")
				}

				// Loops through extension fields
				for fieldPos := 2; fieldPos+3 < len; {
					fieldReadPos := readPos + extPos + 4 + fieldPos
					fieldLen := int((buf[fieldReadPos+1] << 8) + buf[fieldReadPos+2])

					// Is it a hostname field?
					if buf[fieldReadPos] == 0x00 {
						if fieldReadPos+3+fieldLen > pos {
							return errors.New("Not enough data!")
						}

						// Woohoo!
						https.server = string(buf[fieldReadPos+3 : fieldReadPos+3+fieldLen])
						https.tlsBuf = buf[:pos]
						return nil
					}
					fieldPos += 3 + fieldLen
				}
				// There can only be one server name extension
				break
			}
			extPos += 4 + len
		}
		return errors.New("No server name found!")
	}
}

func (https *HttpsProxyConnection) HandleConn() {
	fmt.Printf("Handling new connection from %v\n", https.cConn.RemoteAddr())
	var err error

	// Extract desired server from TLS handshake
	if err = https.ParseTlsHandshake(); err != nil {
		fmt.Printf("Error getting server: %v\n", err)
		https.cConn.Close()
		return
	}

	// TODO(fabi): Figure out what to do if server contains a port number

	// Create connection to server
	fmt.Printf("Connecting to %s:https\n", https.server)
	https.sConn, err = net.Dial("tcp", fmt.Sprintf("%s:https", https.server))
	if err != nil {
		fmt.Printf("Error connecting to server: %v\n", err)
		https.cConn.Close()
		https.sConn.Close()
		return
	}

	// Send data we read during parsing to server
	if _, err = https.sConn.Write(https.tlsBuf); err != nil {
		fmt.Printf("Write error: %v\n", err)
		https.cConn.Close()
		https.sConn.Close()
		return
	}

	// Link both connections together
	fmt.Println("Linking client and server connection")
	go PipeConn(https.cConn, https.sConn)
	go PipeConn(https.sConn, https.cConn)
}

func startHttpProxy() {
	// Start listener
	fmt.Println("Listening on :http")
	listener, err := net.Listen("tcp", ":http")
	if err != nil {
		log.Fatal(err)
	}

	// Wait for clients
	for {
		if conn, err := listener.Accept(); err == nil {
			newConn := HttpProxyConnection{cConn: conn}
			go newConn.HandleConn()
		} else {
			fmt.Printf("Client error: %v\n", err)
		}
	}
}

func startHttpsProxy() {
	// Start listener
	fmt.Println("Listening on :https")
	listener, err := net.Listen("tcp", ":https")
	if err != nil {
		log.Fatal(err)
	}

	// Wait for clients
	for {
		if conn, err := listener.Accept(); err == nil {
			newConn := HttpsProxyConnection{cConn: conn}
			go newConn.HandleConn()
		} else {
			fmt.Printf("Client error: %v\n", err)
		}
	}
}

func main() {
	// Start proxy routines
	go startHttpProxy()
	go startHttpsProxy()

	// Block forever
	select {}
}
