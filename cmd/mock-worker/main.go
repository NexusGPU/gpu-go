package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
)

func main() {
	port := 8080
	// Manually parse os.Args to allow arbitrary flags while looking for -p or --port
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if (arg == "-p" || arg == "--port") && i+1 < len(os.Args) {
			p, err := strconv.Atoi(os.Args[i+1])
			if err == nil {
				port = p
			}
			i++ // Skip the value
		}
	}

	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listening on %s: %v\n", addr, err)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Printf("Mock worker echo server listening on %s...\n", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error accepting connection: %v\n", err)
			continue
		}

		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	fmt.Printf("Accepted connection from %s\n", conn.RemoteAddr())

	// Read first 1K bytes to print and echo
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if n > 0 {
		msg := string(buf[:n])
		fmt.Printf("[%s] Received: %s", conn.RemoteAddr(), msg)
		if n == 1024 {
			fmt.Print("... (truncated)")
		}
		fmt.Println()

		// Send HTTP 200 response instead of echoing
		response := "HTTP/1.1 200 OK\r\n" +
			"Content-Type: text/plain\r\n" +
			"Connection: close\r\n" +
			"\r\n" +
			"this is mock tensor-fusion-worker server\n"

		if _, wErr := conn.Write([]byte(response)); wErr != nil {
			fmt.Fprintf(os.Stderr, "Error sending response to %s: %v\n", conn.RemoteAddr(), wErr)
		}
	}

	// Handle read error if it wasn't EOF
	if err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "Error reading from %s: %v\n", conn.RemoteAddr(), err)
	}

	fmt.Printf("Closing connection from %s\n", conn.RemoteAddr())
}
