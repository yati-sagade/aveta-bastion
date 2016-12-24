This is the host server for Aveta, and is used for data collection. A video
capture process on the robot tries to read frames rapidly and sends them over
a TCP connection (port 9000) to this server, which in turn uses ffmpeg to
encode frames to a video on disk.

Most of the client code (and the line protocol) are lifted from [here][1].

The line protocol is very simple and contains an alternating chain of frame
size encoded as a 4 byte integer in little endian followed by the image data
bytes. A 0 image size indicates end of input. Client expects no reply.

I taped my RPi camera upside down unfortunately, and hence the `vflip` filter
in the ffmpeg incantation (server.go). We are also hard-setting fps to 10, so
frames will be dropped/duplicated as necessary by ffmpeg. I do not think this
will be needed for the actual training, but keeping it anyway since the Python
producer can barely manage >9fps from my experiments. Perhaps a multithreaded
producer as described [here][1] would help.


## Helper scripts

### movedata.pl

Since the server will put data from all sessions in ~/aveta-training-data (with
a unique subdirectory for each session), after collecting data on one type of
track, it is helpful to move this data to another directory, organized by
track-types. `movedata.pl` helps me do that. `perldoc movedata.pl` for usage.


[1]: https://picamera.readthedocs.io/en/release-1.10/recipes1.html#streaming-capture
