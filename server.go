package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"path"

	"github.com/satori/go.uuid"
)

type DataCollectionServer struct {
	Addr      string
	Ffmpeg    string
	OutputDir string
}

func NewDataCollectionServer(addr, ffmpeg, outputDir string) *DataCollectionServer {
	return &DataCollectionServer{Addr: addr, Ffmpeg: ffmpeg, OutputDir: outputDir}
}

func (srv *DataCollectionServer) Start() error {
	l, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}
	defer l.Close()

	if err := os.MkdirAll(srv.OutputDir, 0755); err != nil {
		log.Fatalf("Error creating output directory %s: %s",
			srv.OutputDir, err.Error())
	}

	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go srv.HandleConnection(conn)
	}
}

type timestampedBytes struct {
	Timestamp float64
	Data      []byte
}

func (srv *DataCollectionServer) HandleConnection(c net.Conn) {

	var msgHeader struct {
		Flags         uint8
		TimeStampBits uint64
	}

	connId := uuid.NewV4().String()
	connOutputDir := path.Join(srv.OutputDir, connId)

	if err := os.MkdirAll(connOutputDir, 0755); err != nil {
		log.Fatalf("Could not create directory %s: %s\n",
			connOutputDir, err.Error())
	}

	videoFile := path.Join(connOutputDir, "video.mp4")
	videoQuit := make(chan struct{})
	videoData := make(chan timestampedBytes)
	go srv.VideoWriter(videoFile, videoData, videoQuit)

	cmdFile := path.Join(connOutputDir, "cmd.txt")
	cmdQuit := make(chan struct{})
	cmdData := make(chan timestampedBytes)
	go srv.CmdWriter(cmdFile, cmdData, cmdQuit)

	defer func() {
		cmdQuit <- struct{}{}
		videoQuit <- struct{}{}
	}()

	cmdBytes := make([]byte, 1)
LOOP:
	for {
		err := binary.Read(c, binary.LittleEndian, &msgHeader)

		if err != nil {
			if err == io.EOF {
				break LOOP
			}
			log.Fatalf("Error reading image header: " + err.Error())
		}

		if msgHeader.Flags&0x80 == 0x80 {
			// End of stream
			return
		}

		timeStamp := math.Float64frombits(msgHeader.TimeStampBits)
		if msgHeader.Flags&0x10 == 0 {
			// video frame
			var imgSize uint32
			binary.Read(c, binary.LittleEndian, &imgSize)
			imgBuff := make([]byte, imgSize)
			readSoFar := 0

			for readSoFar != int(imgSize) {
				n, err := c.Read(imgBuff[readSoFar:imgSize])
				if err != nil {
					log.Fatalf("Error reading image data: " + err.Error())
				}
				readSoFar += n
			}
			videoData <- timestampedBytes{timeStamp, imgBuff}
		} else {
			err := binary.Read(c, binary.LittleEndian, &cmdBytes[0])
			if err != nil {
				log.Fatalf("Error reading command byte: " + err.Error())
			}
			cmdData <- timestampedBytes{timeStamp, cmdBytes}
		}
	}
}

func (srv *DataCollectionServer) CmdWriter(outfile string, data chan timestampedBytes, quit chan struct{}) {
	fp, err := os.Create(outfile)
	defer fp.Close()
	if err != nil {
		log.Fatalf("Failed to create output command file %s: %s\n", outfile, err.Error())
	}
	for {
		select {
		case cmdBytes := <-data:
			b := string(cmdBytes.Data[:1])
			s := fmt.Sprintf("%0.2f,%s\n", cmdBytes.Timestamp, b)
			if _, err := fp.WriteString(s); err != nil {
				log.Fatalf("Error writing to command file: %s\n", err.Error())
			}
		case <-quit:
			return
		}
	}
}

func (srv *DataCollectionServer) ffmpegCommand(outfile string) *exec.Cmd {
	ffmpegFilters := `vflip`
	command := exec.Command(srv.Ffmpeg,
		"-f", "mjpeg",
		"-i", "-",
		"-vf", ffmpegFilters,
		"-vcodec", "libx264",
		"-preset", "veryfast",
		"-an",
		"-f", "mp4",
		"-pix_fmt", "yuv420p",
		"-y",
		outfile,
	)
	return command
}

func (srv *DataCollectionServer) startFfmpegProcess(outfile string) (io.WriteCloser, io.ReadCloser, *exec.Cmd) {
	command := srv.ffmpegCommand(outfile)

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
		fatalFromPipe(errPipe, "Error with ffmpeg process: ")
	}
	return inPipe, errPipe, command
}

func (srv *DataCollectionServer) VideoWriter(outfile string, data chan timestampedBytes, quit chan struct{}) {
	syncfile := outfile + ".sync"
	fp, err := os.Create(syncfile)
	if err != nil {
		log.Fatalf("Error creating sync file for video %s: %s\n",
			syncfile, err.Error())
	}
	defer fp.Close()

	inPipe, errPipe, command := srv.startFfmpegProcess(outfile)

	defer func() {
		inPipe.Close()
		command.Wait()
	}()

	var currentTimestamp uint64
	frameCount := 0 // how many frames this second

IMAGE:
	for {
		select {
		case imgBytes := <-data:
			sec := uint64(imgBytes.Timestamp)
			if sec != currentTimestamp {
				if currentTimestamp > 0 && frameCount > 0 {
					s := fmt.Sprintf("%d,%d\n", currentTimestamp, frameCount)
					if _, err := fp.WriteString(s); err != nil {
						log.Fatalf("Error writing to syncfile: %s\n", err.Error())
					}
				}
				currentTimestamp = sec
				frameCount = 0
			}
			_, err = inPipe.Write(imgBytes.Data)
			if err != nil {
				fatalFromPipe(errPipe, "Error with ffmpeg process: ")
			}
			frameCount++
		case <-quit:
			break IMAGE
		}
	}
}

func fatalFromPipe(pipe io.Reader, msg string) {
	buf := new(bytes.Buffer)
	buf.ReadFrom(pipe)
	log.Fatalf(msg + buf.String())
}
