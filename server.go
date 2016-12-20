package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"math"
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
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go srv.HandleConnection(conn)
	}
}

type timestampedBytes struct {
	timestamp float64
	data      []byte
}

func (srv *DataCollectionServer) HandleConnection(c net.Conn) {
	var msgHeader struct {
		Flags         uint8
		TimeStampBits uint64
		ImgSize       uint32
	}
	q := make(chan struct{})
	data := make(chan timestampedBytes)

	go VideoWriter(data, q)
	defer func() {
		q <- struct{}{}
	}()

IMAGE:
	for {
		err := binary.Read(c, binary.LittleEndian, &msgHeader)
		if err != nil {
			if err == io.EOF {
				break IMAGE
			}
			log.Fatalf("Error reading image header: " + err.Error())
		}
		if msgHeader.Flags&0x80 == 0x80 {
			// End of stream
			return
		}
		timeStamp := math.Float64frombits(msgHeader.TimeStampBits)
		imgBuff := make([]byte, msgHeader.ImgSize)
		readSoFar := 0
		for readSoFar != int(msgHeader.ImgSize) {
			n, err := c.Read(imgBuff[readSoFar:msgHeader.ImgSize])
			if err != nil {
				log.Fatalf("Error reading image data: " + err.Error())
			}
			readSoFar += n
		}
		data <- timestampedBytes{timeStamp, imgBuff}
	}
}

func VideoWriter(data chan timestampedBytes, quit chan struct{}) {
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
			_, err = inPipe.Write(imgBytes.data)
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
