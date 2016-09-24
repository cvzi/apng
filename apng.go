// go version go1.7 windows/amd64

// combines multiple png file into one apng animation file
// keep in mind: this is my first go program ever
// info about reading/writing PNG format
// http://golang.org/src/pkg/image/png/writer.go
// http://golang.org/src/pkg/image/png/reader.go

package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
)

const pngHeader = "\x89PNG\r\n\x1a\n"
const maxChunkSize = 1024 * 1024 // in byte

type decoder struct {
	r         io.Reader
	crc       hash.Hash32
	ChunkName string
	tmp       [maxChunkSize]byte
}

type FormatError string

func (e FormatError) Error() string { return "png: invalid format: " + string(e) }

type UnsupportedError string

func (e UnsupportedError) Error() string { return "png: unsupported feature: " + string(e) }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (d *decoder) parseChunk() (uint32, error) {
	// Read the length and chunk type.
	_, err := io.ReadFull(d.r, d.tmp[0:8])
	if err != nil {
		return 0, err
	}
	length := binary.BigEndian.Uint32(d.tmp[0:4])

	d.ChunkName = string(d.tmp[4:8])

	//fmt.Printf("%s length %d\n", d.ChunkName, length)

	// Read chunk data and 4 bytes crc checksum
	_, err = io.ReadFull(d.r, d.tmp[8:length+8+4])
	if err != nil {
		return 0, err
	}
	return length + 8 + 4, nil
}

func (d *decoder) checkHeader() error {
	_, err := io.ReadFull(d.r, d.tmp[:len(pngHeader)])
	if err != nil {
		return err
	}
	if string(d.tmp[:len(pngHeader)]) != pngHeader {
		return FormatError("not a PNG file")
	}
	return nil
}

type encoder struct {
	w               io.Writer
	err             error
	header          [8]byte
	footer          [4]byte
	tmp             [maxChunkSize]byte
	animationChunks uint32 // count animation chunks (fcTL and fdAT), starting with the first fcTL as 0. This is often referred to as SEQUENCE NUMBER
}

// Big-endian.
func writeUint32(b []uint8, u uint32) {
	b[0] = uint8(u >> 24)
	b[1] = uint8(u >> 16)
	b[2] = uint8(u >> 8)
	b[3] = uint8(u >> 0)
}
func writeUint16(b []uint8, u uint16) {
	b[0] = uint8(u >> 8)
	b[1] = uint8(u >> 0)
}

func (e *encoder) writeChunk(b []byte, name string) {
	if e.err != nil {
		return
	}
	n := uint32(len(b))
	if int(n) != len(b) {
		e.err = UnsupportedError(name + " chunk is too large: " + strconv.Itoa(len(b)))
		return
	}
	writeUint32(e.header[:4], n)
	e.header[4] = name[0]
	e.header[5] = name[1]
	e.header[6] = name[2]
	e.header[7] = name[3]
	crc := crc32.NewIEEE()
	crc.Write(e.header[4:8])
	crc.Write(b)
	writeUint32(e.footer[:4], crc.Sum32())

	_, e.err = e.w.Write(e.header[:8])
	if e.err != nil {
		return
	}
	_, e.err = e.w.Write(b)
	if e.err != nil {
		return
	}
	_, e.err = e.w.Write(e.footer[:4])
}

func (e *encoder) writeIEND() {
	e.writeChunk(nil, "IEND")
}

func (e *encoder) writeACTL(framenumber, loop int) {
	// https://wiki.mozilla.org/APNG_Specification#.60acTL.60:_The_Animation_Control_Chunk
	writeUint32(e.tmp[0:4], uint32(framenumber)) // number of actual FRAMES. minimum is 1. THIS IS NOT THE SEQUENCE NUMBER AKA ANIMATION CHUNK NUMBER
	writeUint32(e.tmp[4:8], uint32(loop))        // Number of times to loop this APNG.  0 indicates infinite looping.
	e.writeChunk(e.tmp[:8], "acTL")
}

func (e *encoder) writeFCTL(seqnumber uint32, width int32, height int32, delay int) {
	// https://wiki.mozilla.org/APNG_Specification#.60fcTL.60:_The_Frame_Control_Chunk
	writeUint32(e.tmp[0:4], seqnumber)       // Sequence number of the animation chunk, starting from 0. Animation chunks are both fcTL and fdAT
	writeUint32(e.tmp[4:8], uint32(width))   // Width of the following frame
	writeUint32(e.tmp[8:12], uint32(height)) // Height of the following frame
	writeUint32(e.tmp[12:16], uint32(0))     // X position at which to render the following frame
	writeUint32(e.tmp[16:20], uint32(0))     // Y position at which to render the following frame
	writeUint16(e.tmp[20:22], uint16(delay)) // Frame delay fraction numerator
	// If the denominator is 0, it is to be treated as if it were 100 (that is, `delay_num` then specifies 1/100ths of a second)
	writeUint16(e.tmp[22:24], uint16(0)) // Frame delay fraction denominator
	e.tmp[24] = 0                        // Type of frame area disposal to be done after rendering this frame
	e.tmp[25] = 0                        // Type of frame area rendering for this frame
	e.writeChunk(e.tmp[:26], "fcTL")
	//fmt.Printf("seqnumber: %d",seqnumber)
}

func (e *encoder) copyIDAT(filename string, width int32, height int32, delay int) {
	e.animationChunks = 0

	// Copy all IDAT chunks of the first png file into the "encoder file"
	r, err := os.Open(filename)
	if err != nil {
		log.Fatalf("Could not open frame file: %s", filename)
	}
	defer r.Close()

	d := &decoder{
		r:   r,
		crc: crc32.NewIEEE(),
	}

	// check header
	if err := d.checkHeader(); err != nil {
		log.Fatalf("No PNG header found in %s", filename)
	}

	// Skip over IHDR
	var length uint32
	length, err = d.parseChunk()
	if err != nil {
		log.Fatalf("Could not read IHDR of %s", filename)
	}

	// Write frame
	e.writeFCTL(e.animationChunks, width, height, delay)
	e.animationChunks++

	// Read all IDAT chunks and convert them into bigger IDAT chunks
	maxfdATlength := uint32(maxChunkSize - 5*4)
	var buffer []byte = make([]byte, maxfdATlength)
	buffer = buffer[:0]
	for {
		length, err = d.parseChunk()
		if d.ChunkName == "IDAT" {
			if uint32(len(buffer))+length > maxfdATlength {
				// Write new IDAT chunk
				e.writeChunk(buffer, "IDAT")
				//fmt.Printf("seqnumber: %d\n",e.animationChunks)

				// Clear buffer
				buffer = buffer[:0]
			}

			// Write the current IDAT stripping the leading length and chunk identifier and the trailing CRC checksum
			buffer = append(buffer, d.tmp[8:length-4]...)

		}
		if d.ChunkName == "IEND" {
			break
		}
	}
	// Write last chunk
	if len(buffer) > 4 {
		e.writeChunk(buffer[:], "IDAT")
	}

}

func (e *encoder) writeFDAT(filename string, width int32, height int32, delay int) {
	//https://wiki.mozilla.org/APNG_Specification#.60fdAT.60:_The_Frame_Data_Chunk

	r, err := os.Open(filename)
	if err != nil {
		log.Fatalf("Could not open frame file: %s", filename)
	}
	defer r.Close()

	d := &decoder{
		r:   r,
		crc: crc32.NewIEEE(),
	}

	// check header
	if err := d.checkHeader(); err != nil {
		log.Fatalf("No PNG header found in %s", filename)
	}

	// Skip over IHDR
	var length uint32
	length, err = d.parseChunk()
	if err != nil {
		log.Fatalf("Could not read IHDR of %s", filename)
	}

	// Write frame
	//e.writeFCTL(seqnumber,width,height,delay)
	e.writeFCTL(e.animationChunks, width, height, delay)
	e.animationChunks++

	// Read all IDAT chunks and convert them into fdAT chunks
	var fourbytes []byte = make([]byte, 4)
	maxfdATlength := uint32(maxChunkSize - 5*4) //    minus:  4byteoffset,length,"fdAT",seqnumber,crc
	var buffer []byte = make([]byte, maxfdATlength)
	buffer = buffer[:0]

	writeUint32(fourbytes, e.animationChunks) // Write with the sequence number at the beginning
	buffer = append(buffer, fourbytes...)
	e.animationChunks++
	for {
		length, err = d.parseChunk()
		if d.ChunkName == "IDAT" {
			if uint32(len(buffer))+length > maxfdATlength {
				// Write fdAT chunk
				e.writeChunk(buffer, "fdAT")

				// Clear buffer
				buffer = buffer[:0]
				writeUint32(fourbytes, e.animationChunks) // Write with the sequence number at the beginning
				buffer = append(buffer, fourbytes...)
				e.animationChunks++
			}

			// Write the current IDAT stripping the leading length and chunk identifier and the trailing CRC checksum
			buffer = append(buffer, d.tmp[8:length-4]...)

		}
		if d.ChunkName == "IEND" {
			break
		}
	}
	// Write last chunk
	if len(buffer) > 4 {
		e.writeChunk(buffer[:], "fdAT")
		//fmt.Printf("seqnumber: %d\n",e.animationChunks)
	} else {
		// last chunk contains no data, only the sequence number, so we discard the last chunk
		e.animationChunks--
	}
}

// Encode writes all the png files in frames into the output file w.
// All the png files must be encoded in the same manner and must have the same dimensions.
// (Meaning each file should have the same IHDR chunk because only the first IHDR is evaluated)
func Encode(w io.Writer, pngfiles []string, delays []int) {
	e := &encoder{
		w: w,
	}

	// Open first frame
	r, err := os.Open(pngfiles[0])
	if err != nil {
		log.Fatalf("Could not open first frame file: %s", pngfiles[0])
	}
	defer r.Close()

	d := &decoder{
		r:   r,
		crc: crc32.NewIEEE(),
	}

	var length uint32 // chunk length

	// check header of first frame
	if err := d.checkHeader(); err != nil {
		log.Fatalf("No PNG header found!")
	}

	// Write png header to output
	e.w.Write(d.tmp[0:8])

	// Copy IHDR from first frame to output
	length, err = d.parseChunk()
	if err != nil {
		log.Fatalf("Could not read IHDR from first file.")
	}
	e.w.Write(d.tmp[0:length])
	width := int32(binary.BigEndian.Uint32(d.tmp[8:12]))
	height := int32(binary.BigEndian.Uint32(d.tmp[12:16]))

	fmt.Printf("Image dimensions: %d x %d\n", width, height)

	fmt.Printf("Encoding: %s\n", pngfiles[0])

	// Write ACTL chunk
	e.writeACTL(len(pngfiles), 0)

	// Write first image
	e.copyIDAT(pngfiles[0], width, height, delays[0])

	// Read/Write the other files
	for i := 1; i < len(pngfiles); i++ {
		fmt.Printf("Encoding: %s\n", pngfiles[i])
		e.writeFDAT(pngfiles[i], width, height, delays[i])
	}

	// Write End chunk
	e.writeIEND()

	fmt.Printf("Wrote %d frames split up in %d animation chunks\n", len(pngfiles), e.animationChunks)
}

// Readln returns a single line (without the ending \n)
// from the input buffered reader.
// An error is returned iff there is an error with the
// buffered reader.
func Readln(r *bufio.Reader) (string, error) {
	var (
		isPrefix bool  = true
		err      error = nil
		line, ln []byte
	)
	for isPrefix && err == nil {
		line, isPrefix, err = r.ReadLine()
		ln = append(ln, line...)
	}
	return string(ln), err
}

// Command line arguments
var dirname string

func init() {
	const (
		defaultDirname = "frames"
		usage          = "The folder containing the source PNG files"
	)
	flag.StringVar(&dirname, "input", defaultDirname, usage)
	flag.StringVar(&dirname, "i", defaultDirname, usage+"-input (shorthand)")
}

var delayfile string

func init() {
	const (
		defaultDelayfile = "delays.txt"
		usage            = "A text file containing the duration of each frame in milliseconds. Split by line."
	)
	flag.StringVar(&delayfile, "delays", defaultDelayfile, usage)
	flag.StringVar(&delayfile, "d", defaultDelayfile, "-delays (shorthand)")
}

var output string

func init() {
	const (
		defaultOutput = "output.png"
		usage         = "The destination file."
	)
	flag.StringVar(&output, "output", defaultOutput, usage)
	flag.StringVar(&output, "o", defaultOutput, "-output (shorthand)")
}

func main() {

	flag.Parse()

	globaldelay := 100 // unit is ms

	// Read the delays
	readdelays := make([]int, 0)
	f, err := os.Open(delayfile)
	if err != nil {
		fmt.Printf("error opening file: %v\n", err)
	} else {
		r := bufio.NewReader(f)
		s, e := Readln(r)
		for e == nil {
			i, e2 := strconv.Atoi(s)
			if e2 == nil {
				readdelays = append(readdelays, i/10)
			}
			s, e = Readln(r)
		}
	}

	// Find all png files
	list, err := ioutil.ReadDir(dirname)
	if err != nil {
		log.Fatalf("ReadDir: Could not read %s", dirname)
	}
	pngfiles := make([]string, 0)
	delays := make([]int, 0)
	delays = append(delays, readdelays...)

	for _, value := range list {
		if strings.HasSuffix(value.Name(), ".png") {
			pngfiles = append(pngfiles, dirname+"/"+value.Name())
			if len(delays) < len(pngfiles) {
				delays = append(delays, globaldelay/10)
			}
		}
	}

	// Open output file
	w, err := os.Create(output)
	if err != nil {
		log.Fatalf("Could not open output file: %s", output)
	}
	defer w.Close()

	Encode(w, pngfiles, delays)

	fmt.Printf("End\n")
}
