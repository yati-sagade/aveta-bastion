package main

func main() {
	srv := NewDataCollectionServer("0.0.0.0:9000")
	srv.Start()
}
