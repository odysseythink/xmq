package main

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"mlib.com/go-xmq"
	"mlib.com/mlog"
)

type FileLogger struct {
	opts     *Options
	topic    string
	consumer *xmq.Consumer

	out            *os.File
	writer         io.Writer
	gzipWriter     *gzip.Writer
	logChan        chan *xmq.Message
	filenameFormat string

	termChan chan bool
	hupChan  chan bool

	// for rotation
	filename string
	openTime time.Time
	filesize int64
	rev      uint
}

func NewFileLogger(opts *Options, topic string, cfg *xmq.Config) (*FileLogger, error) {
	computedFilenameFormat, err := computeFilenameFormat(opts, topic)
	if err != nil {
		return nil, err
	}

	consumer, err := xmq.NewConsumer(topic, opts.Channel, cfg)
	if err != nil {
		return nil, err
	}

	f := &FileLogger{
		opts:           opts,
		topic:          topic,
		consumer:       consumer,
		logChan:        make(chan *xmq.Message, 1),
		filenameFormat: computedFilenameFormat,
		termChan:       make(chan bool),
		hupChan:        make(chan bool),
	}
	consumer.AddHandler(f)
	err = consumer.ConnectToXMQDs(opts.XMQDTCPAddrs)
	if err != nil {
		return nil, err
	}

	err = consumer.ConnectToXMQLookupds(opts.XMQLookupdHTTPAddrs)
	if err != nil {
		return nil, err
	}

	return f, nil
}

func (f *FileLogger) HandleMessage(m *xmq.Message) error {
	m.DisableAutoResponse()
	f.logChan <- m
	return nil
}

func (f *FileLogger) router() {
	pos := 0
	output := make([]*xmq.Message, f.opts.MaxInFlight)
	sync := false
	ticker := time.NewTicker(f.opts.SyncInterval)
	closeFile := false
	exit := false

	for {
		select {
		case <-f.consumer.StopChan:
			sync = true
			closeFile = true
			exit = true
		case <-f.termChan:
			ticker.Stop()
			f.consumer.Stop()
			sync = true
		case <-f.hupChan:
			sync = true
			closeFile = true
		case <-ticker.C:
			if f.needsRotation() {
				if f.opts.SkipEmptyFiles {
					closeFile = true
				} else {
					f.updateFile()
				}
			}
			sync = true
		case m := <-f.logChan:
			if f.needsRotation() {
				f.updateFile()
				sync = true
			}
			_, err := f.Write(m.Body)
			if err != nil {
				mlog.Fatalf("[%s/%s] writing message to disk: %s", f.topic, f.opts.Channel, err)
				os.Exit(1)
			}
			_, err = f.Write([]byte("\n"))
			if err != nil {
				mlog.Fatalf("[%s/%s] writing newline to disk: %s", f.topic, f.opts.Channel, err)
				os.Exit(1)
			}
			output[pos] = m
			pos++
			if pos == cap(output) {
				sync = true
			}
		}

		if sync || f.consumer.IsStarved() {
			if pos > 0 {
				mlog.Infof("[%s/%s] syncing %d records to disk", f.topic, f.opts.Channel, pos)
				err := f.Sync()
				if err != nil {
					mlog.Fatalf("[%s/%s] failed syncing messages: %s", f.topic, f.opts.Channel, err)
					os.Exit(1)
				}
				for pos > 0 {
					pos--
					m := output[pos]
					m.Finish()
					output[pos] = nil
				}
			}
			sync = false
		}

		if closeFile {
			f.Close()
			closeFile = false
		}

		if exit {
			break
		}
	}
}

func (f *FileLogger) Close() {
	if f.out == nil {
		return
	}

	if f.gzipWriter != nil {
		err := f.gzipWriter.Close()
		if err != nil {
			mlog.Fatalf("[%s/%s] failed to close GZIP writer: %s", f.topic, f.opts.Channel, err)
			os.Exit(1)
		}
	}
	err := f.out.Sync()
	if err != nil {
		mlog.Fatalf("[%s/%s] failed to fsync output file: %s", f.topic, f.opts.Channel, err)
		os.Exit(1)
	}
	err = f.out.Close()
	if err != nil {
		mlog.Fatalf("[%s/%s] failed to close output file: %s", f.topic, f.opts.Channel, err)
		os.Exit(1)
	}

	// Move file from work dir to output dir if necessary, taking care not
	// to overwrite existing files
	if f.opts.WorkDir != f.opts.OutputDir {
		src := f.out.Name()
		dst := filepath.Join(f.opts.OutputDir, strings.TrimPrefix(src, f.opts.WorkDir))

		// Optimistic rename
		mlog.Infof("[%s/%s] moving finished file %s to %s", f.topic, f.opts.Channel, src, dst)
		err := exclusiveRename(src, dst)
		if err == nil {
			return
		} else if !os.IsExist(err) {
			mlog.Fatalf("[%s/%s] unable to move file from %s to %s: %s", f.topic, f.opts.Channel, src, dst, err)
			os.Exit(1)
		}

		// Optimistic rename failed, so we need to generate a new
		// destination file name by bumping the revision number.
		_, filenameTmpl := filepath.Split(f.filename)
		dstDir, _ := filepath.Split(dst)
		dstTmpl := filepath.Join(dstDir, filenameTmpl)

		for i := f.rev + 1; ; i++ {
			mlog.Warningf("[%s/%s] destination file already exists: %s", f.topic, f.opts.Channel, dst)
			dst := strings.Replace(dstTmpl, "<REV>", fmt.Sprintf("-%06d", i), -1)
			err := exclusiveRename(src, dst)
			if err != nil {
				if os.IsExist(err) {
					continue // next rev
				}
				mlog.Fatalf("[%s/%s] unable to rename file from %s to %s: %s", f.topic, f.opts.Channel, src, dst, err)
				os.Exit(1)
			}
			mlog.Infof("[%s/%s] renamed finished file %s to %s to avoid overwrite", f.topic, f.opts.Channel, src, dst)
			break
		}
	}

	f.out = nil
}

func (f *FileLogger) Write(p []byte) (int, error) {
	n, err := f.writer.Write(p)
	f.filesize += int64(n)
	return n, err
}

func (f *FileLogger) Sync() error {
	var err error
	if f.gzipWriter != nil {
		// finish current gzip stream and start a new one (concatenated)
		// gzip stream trailer has checksum, and can indicate which messages were ACKed
		err = f.gzipWriter.Close()
		if err != nil {
			return err
		}
		err = f.out.Sync()
		f.gzipWriter, _ = gzip.NewWriterLevel(f.out, f.opts.GZIPLevel)
		f.writer = f.gzipWriter
	} else {
		err = f.out.Sync()
	}
	return err
}

func (f *FileLogger) currentFilename() string {
	t := time.Now()
	datetime := strftime(f.opts.DatetimeFormat, t)
	return strings.Replace(f.filenameFormat, "<DATETIME>", datetime, -1)
}

func (f *FileLogger) needsRotation() bool {
	if f.out == nil {
		return true
	}

	filename := f.currentFilename()
	if filename != f.filename {
		mlog.Infof("[%s/%s] new filename %s, rotating...", f.topic, f.opts.Channel, filename)
		return true // rotate by filename
	}

	if f.opts.RotateInterval > 0 {
		if s := time.Since(f.openTime); s > f.opts.RotateInterval {
			mlog.Infof("[%s/%s] %s since last open, rotating...", f.topic, f.opts.Channel, s)
			return true // rotate by interval
		}
	}

	if f.opts.RotateSize > 0 && f.filesize > f.opts.RotateSize {
		mlog.Infof("[%s/%s] %s currently %d bytes (> %d), rotating...",
			f.topic, f.opts.Channel, f.out.Name(), f.filesize, f.opts.RotateSize)
		return true // rotate by size
	}

	return false
}

func (f *FileLogger) updateFile() {
	f.Close() // uses current f.filename and f.rev to resolve rename dst conflict

	filename := f.currentFilename()
	if filename != f.filename {
		f.rev = 0 // reset revision to 0 if it is a new filename
	} else {
		f.rev++
	}
	f.filename = filename
	f.openTime = time.Now()

	fullPath := path.Join(f.opts.WorkDir, filename)
	err := makeDirFromPath(fullPath)
	if err != nil {
		mlog.Fatalf("[%s/%s] unable to create dir: %s", f.topic, f.opts.Channel, err)
		os.Exit(1)
	}

	var fi os.FileInfo
	for ; ; f.rev++ {
		absFilename := strings.Replace(fullPath, "<REV>", fmt.Sprintf("-%06d", f.rev), -1)

		// If we're using a working directory for in-progress files,
		// proactively check for duplicate file names in the output dir to
		// prevent conflicts on rename in the normal case
		if f.opts.WorkDir != f.opts.OutputDir {
			outputFileName := filepath.Join(f.opts.OutputDir, strings.TrimPrefix(absFilename, f.opts.WorkDir))
			err := makeDirFromPath(outputFileName)
			if err != nil {
				mlog.Fatalf("[%s/%s] unable to create dir: %s", f.topic, f.opts.Channel, err)
				os.Exit(1)
			}

			_, err = os.Stat(outputFileName)
			if err == nil {
				mlog.Warningf("[%s/%s] output file already exists: %s", f.topic, f.opts.Channel, outputFileName)
				continue // next rev
			} else if !os.IsNotExist(err) {
				mlog.Fatalf("[%s/%s] unable to stat output file %s: %s", f.topic, f.opts.Channel, outputFileName, err)
				os.Exit(1)
			}
		}

		openFlag := os.O_WRONLY | os.O_CREATE
		if f.opts.GZIP || f.opts.RotateInterval > 0 {
			openFlag |= os.O_EXCL
		} else {
			openFlag |= os.O_APPEND
		}
		f.out, err = os.OpenFile(absFilename, openFlag, 0666)
		if err != nil {
			if os.IsExist(err) {
				mlog.Warningf("[%s/%s] working file already exists: %s", f.topic, f.opts.Channel, absFilename)
				continue // next rev
			}
			mlog.Fatalf("[%s/%s] unable to open %s: %s", f.topic, f.opts.Channel, absFilename, err)
			os.Exit(1)
		}

		mlog.Infof("[%s/%s] opening %s", f.topic, f.opts.Channel, absFilename)

		fi, err = f.out.Stat()
		if err != nil {
			mlog.Fatalf("[%s/%s] unable to stat file %s: %s", f.topic, f.opts.Channel, f.out.Name(), err)
		}
		f.filesize = fi.Size()

		if f.opts.RotateSize > 0 && f.filesize > f.opts.RotateSize {
			mlog.Infof("[%s/%s] %s currently %d bytes (> %d), rotating...",
				f.topic, f.opts.Channel, f.out.Name(), f.filesize, f.opts.RotateSize)
			continue // next rev
		}

		break // good file
	}

	if f.opts.GZIP {
		f.gzipWriter, _ = gzip.NewWriterLevel(f.out, f.opts.GZIPLevel)
		f.writer = f.gzipWriter
	} else {
		f.writer = f.out
	}
}

func makeDirFromPath(path string) error {
	dir, _ := filepath.Split(path)
	if dir != "" {
		return os.MkdirAll(dir, 0770)
	}
	return nil
}

func exclusiveRename(src, dst string) error {
	err := os.Link(src, dst)
	if err != nil {
		return err
	}

	err = os.Remove(src)
	if err != nil {
		return err
	}

	return nil
}

func computeFilenameFormat(opts *Options, topic string) (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}
	shortHostname := strings.Split(hostname, ".")[0]

	identifier := shortHostname
	if len(opts.HostIdentifier) != 0 {
		identifier = strings.Replace(opts.HostIdentifier, "<SHORT_HOST>", shortHostname, -1)
		identifier = strings.Replace(identifier, "<HOSTNAME>", hostname, -1)
	}

	cff := opts.FilenameFormat
	if opts.GZIP || opts.RotateSize > 0 || opts.RotateInterval > 0 || opts.WorkDir != opts.OutputDir {
		if !strings.Contains(cff, "<REV>") {
			return "", errors.New("missing <REV> in --filename-format when gzip, rotation, or work dir enabled")
		}
	} else {
		// remove <REV> as we don't need it
		cff = strings.Replace(cff, "<REV>", "", -1)
	}
	cff = strings.Replace(cff, "<TOPIC>", topic, -1)
	cff = strings.Replace(cff, "<HOST>", identifier, -1)
	cff = strings.Replace(cff, "<PID>", fmt.Sprintf("%d", os.Getpid()), -1)
	if opts.GZIP && !strings.HasSuffix(cff, ".gz") {
		cff = cff + ".gz"
	}

	return cff, nil
}
