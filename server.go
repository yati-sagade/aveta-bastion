package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path"
	"time"
)

const (
	VideoFilename = "video.avi"
	SyncFilename  = "sync.txt"
	CmdFilename   = "commands.txt"
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

	rand.Seed(time.Now().Unix())

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

type rawCmd struct {
	Cmd        byte
	LeftSpeed  int16
	RightSpeed int16
}

type timestampedCmd struct {
	Timestamp float64
	rawCmd
}

func (srv *DataCollectionServer) createConnOutputDir() string {
	now := time.Now().UTC().Format(time.RFC3339)
	randIdent := rand.Int63()
	connId := fmt.Sprintf("%s_%d", now, randIdent)
	connOutputDir := path.Join(srv.OutputDir, connId)

	if err := os.MkdirAll(connOutputDir, 0755); err != nil {
		log.Fatalf("Could not create directory %s: %s\n",
			connOutputDir, err.Error())
	}
	return connOutputDir
}

type msgHeader struct {
	Flags         uint8
	TimeStampBits uint64
}

// Return if we have reached end of stream
func readMsgHeader(c net.Conn, s *msgHeader) bool {
	err := binary.Read(c, binary.LittleEndian, s)
	if err != nil {
		if err == io.EOF {
			return true
		}
		log.Fatalf("Error reading image header: " + err.Error())
	}
	return false
}

func (srv *DataCollectionServer) HandleConnection(c net.Conn) {

	var header msgHeader
	connOutputDir := srv.createConnOutputDir()

	videoQuit := make(chan struct{})
	videoData := make(chan timestampedBytes)
	go srv.VideoWriter(connOutputDir, videoData, videoQuit)

	cmdQuit := make(chan struct{})
	cmdData := make(chan timestampedCmd)
	go srv.CmdWriter(connOutputDir, cmdData, cmdQuit)

	defer func() {
		cmdQuit <- struct{}{}
		videoQuit <- struct{}{}
	}()

LOOP:
	for {
		if sawEof := readMsgHeader(c, &header); sawEof {
			break LOOP
		}

		if header.Flags&0x80 == 0x80 {
			// End of stream
			return
		}

		timeStamp := math.Float64frombits(header.TimeStampBits)
		if header.Flags&0x01 == 0 {
			// video frame
			videoData <- readVideoFrame(c, timeStamp)
		} else {
			cmdData <- readCmdMsg(c, timeStamp)
		}
	}
}

func readCmdMsg(c net.Conn, timeStamp float64) timestampedCmd {
	var msg timestampedCmd
	msg.Timestamp = timeStamp
	err := binary.Read(c, binary.LittleEndian, &msg.rawCmd)
	if err != nil {
		log.Fatalf("Error reading command message: " + err.Error())
	}
	return msg
}

func readVideoFrame(c net.Conn, timeStamp float64) timestampedBytes {
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
	return timestampedBytes{timeStamp, imgBuff}
}

func (srv *DataCollectionServer) CmdWriter(connOutputDir string, data chan timestampedCmd, quit chan struct{}) {
	outfile := path.Join(connOutputDir, CmdFilename)
	fp, err := os.Create(outfile)
	defer fp.Close()
	if err != nil {
		log.Fatalf("Failed to create output command file %s: %s\n", outfile, err.Error())
	}
	for {
		select {
		case cmd := <-data:
			b := string(cmd.Cmd)

			s := fmt.Sprintf("%0.2f,%s,%d,%d\n",
				cmd.Timestamp, b, cmd.LeftSpeed, cmd.RightSpeed)

			if _, err := fp.WriteString(s); err != nil {
				log.Fatalf("Error writing to command file: %s\n", err.Error())
			}
		case <-quit:
			return
		}
	}
}

func (srv *DataCollectionServer) ffmpegCommand(outfile string) *exec.Cmd {
	// flip around both axes and then crop to only keep the lower half.
	ffmpegFilters := `vflip,hflip,crop=in_w:in_h/2`
	command := exec.Command(srv.Ffmpeg,
		"-f", "mjpeg",
		"-i", "-",
		"-vf", ffmpegFilters,
		"-vcodec", "libx264",
		"-preset", "veryfast",
		"-an",
		"-f", "avi",
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

func writeSyncFileLine(out *os.File, timestamp uint64, count int) {
	s := fmt.Sprintf("%d,%d\n", timestamp, count)
	if _, err := out.WriteString(s); err != nil {
		log.Fatalf("Error writing to syncfile: %s\n", err.Error())
	}
}

func (srv *DataCollectionServer) VideoWriter(connOutputDir string, data chan timestampedBytes, quit chan struct{}) {
	outfile := path.Join(connOutputDir, VideoFilename)
	syncfile := path.Join(connOutputDir, SyncFilename)
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
			if currentTimestamp == 0 {
				currentTimestamp = sec
			}
			if sec != currentTimestamp {
				if frameCount > 0 {
					writeSyncFileLine(fp, currentTimestamp, frameCount)
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
	if frameCount > 0 {
		writeSyncFileLine(fp, currentTimestamp, frameCount)
	}
}

func fatalFromPipe(pipe io.Reader, msg string) {
	buf := new(bytes.Buffer)
	buf.ReadFrom(pipe)
	log.Fatalf(msg + buf.String())
}
