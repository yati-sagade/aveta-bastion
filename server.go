package main

import (
	"bytes"
	"encoding/binary"
	"log"
	"net"
	"os/exec"
)

type DataCollectionServer struct {
	Addr string
}

func NewDataCollectionServer(addr string) *DataCollectionServer {
	return &DataCollectionServer{Addr: addr}
}

func (srv *DataCollectionServer) Start() error {
	l, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}
	defer l.Close()
	log.Println("Listening for connections on " + srv.Addr)
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go srv.HandleConnection(conn)
	}
}

func (srv *DataCollectionServer) HandleConnection(c net.Conn) {
	var imgSize int32
	q := make(chan struct{})
	data := make(chan []byte)
	go VideoWriter(data, q)
IMAGE:
	for {
		err := binary.Read(c, binary.LittleEndian, &imgSize)
		if err != nil {
			log.Fatalf("Error reading image size: " + err.Error())
		}
		if imgSize == 0 {
			break IMAGE
		}
		imgBuff := make([]byte, imgSize)
		readSoFar := 0
		for readSoFar != int(imgSize) {
			n, err := c.Read(imgBuff[readSoFar:imgSize])
			if err != nil {
				log.Fatalf("Error reading image data: " + err.Error())
			}
			readSoFar += n
		}
		data <- imgBuff
	}
	q <- struct{}{}
}

func VideoWriter(data chan []byte, quit chan struct{}) {
	drawTextFilter := `vflip, fps=10, drawtext=fontfile=/usr/share/fonts/dejavu/DejaVuSans-Bold.ttf: text='%{localtime\:%s}': fontcolor=white@1.0: x=100: y=100`
	command := exec.Command("/usr/bin/ffmpeg",
		"-f", "mjpeg",
		"-i", "-",
		"-vf", drawTextFilter,
		"-vcodec", "libx264",
		"-preset", "veryfast",
		"-an",
		"-f", "mp4",
		"-pix_fmt", "yuv420p",
		"-y",
		"/home/ys/output-go.mp4",
	)
	inPipe, err := command.StdinPipe()
	if err != nil {
		panic(err)
	}

	errPipe, err := command.StderrPipe()
	if err != nil {
		panic(err)
	}

	err = command.Start()
	if err != nil {
		buf := new(bytes.Buffer)
		buf.ReadFrom(errPipe)
		log.Fatalf("Error with ffmpeg process: " + buf.String())
	}

	defer func() {
		inPipe.Close()
		command.Wait()
	}()

IMAGE:
	for {
		select {
		case imgBytes := <-data:
			_, err = inPipe.Write(imgBytes)
			if err != nil {
				buf := new(bytes.Buffer)
				buf.ReadFrom(errPipe)
				log.Fatalf("Error with ffmpeg process: " + buf.String())
			}
		case <-quit:
			break IMAGE
		}
	}
}
