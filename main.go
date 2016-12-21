package main

func main() {
	srv := NewDataCollectionServer(
		"0.0.0.0:9000",
		"/usr/bin/ffmpeg",
		"/home/ys/aveta-training-data",
	)
	srv.Start()
	//StartTimetampReceiver()
}
