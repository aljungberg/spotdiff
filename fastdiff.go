package main

import (
	"bytes"
	"fmt"
	tm "github.com/buger/goterm"
	flag "github.com/ogier/pflag"
	"github.com/pkg/profile"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

type NameAndSize struct {
	name string
	size int64
}

type Stats struct {
	aCount   int64
	bCount   int64
	aMissing int64
	bMissing int64

	totalSize             int64
	unreadable            int64
	notEqual              int64
	completeCheck         int64
	partiallyCheckedCount int64
	partiallyCheckedBytes int64
	partiallyCheckedSize  int64
}

const BLOCK_SIZE int64 = 4096
const FOLDER_LISTING_PIPELINE_DEPTH = 16
const FOLDER_ENTRY_READ_AHEAD = 64
const DATA_DIFF_PIPELINE_DEPTH = 128
const PROGRESS_FREQUENCY = 200 * time.Millisecond

type Logger struct {
	onProgressLine     bool
	terminalWidth      int
	quiet              bool
	nextProgressFormat string
	nextProgressArgs   []interface{}
	mutex              sync.Mutex
}

var logger Logger

func (l *Logger) progress(format string, args ...interface{}) {
	time.Sleep(100 * time.Millisecond)

	l.mutex.Lock()
	l.nextProgressFormat = format
	l.nextProgressArgs = args
	l.mutex.Unlock()
}

func (l *Logger) run(quit chan int) {
	for {
		select {
		case <-quit:
			return
		default:
			text := ""
			l.mutex.Lock()
			if l.nextProgressFormat != "" {
				text = fmt.Sprintf(l.nextProgressFormat, l.nextProgressArgs...)
			}
			l.nextProgressFormat = ""
			l.mutex.Unlock()
			// Do this after we released the lock.
			if text != "" {
				l._progress(text)
			}
			time.Sleep(PROGRESS_FREQUENCY)
		}
	}
}

func (l *Logger) _progress(text string) {
	if l.quiet {
		return
	}

	if l.terminalWidth == 0 {
		// TODO Refresh if the terminal changes width.
		l.terminalWidth = tm.Width()
	}
	os.Stderr.WriteString("\r\033[K" + text[:min(l.terminalWidth-1, len(text))])
	l.onProgressLine = true
}

func (l *Logger) resetIfNeeded() {
	if l.onProgressLine {
		// tm.ResetLine("")
		os.Stderr.WriteString("\r\033[K")
		l.onProgressLine = false
	}
}

// Write output to stderr. This method should be used for "real" errors, not informative messages.
func (l *Logger) error(text string) {
	l.resetIfNeeded()
	os.Stderr.WriteString(text + "\n")
}

// Write output to stdout. This method should be used for "real" output, not informative messages: the kind of stuff
// a user might want to pipe to a file if they want just a list of differences.
func (l *Logger) writeOut(text string) {
	l.resetIfNeeded()
	os.Stdout.WriteString(text + "\n")
}

// Write an informative message to stderr. The message will be suppressed in quiet mode.
func (l *Logger) log(text string) {
	if l.quiet {
		return
	}

	l.resetIfNeeded()
	os.Stderr.WriteString(text + "\n")
}

func listInto(results chan os.FileInfo, rootPath string, relPath string) {
	defer close(results)

	// It's important that the results are sorted.
	files, err := ioutil.ReadDir(path.Join(rootPath, relPath))

	if err != nil {
		logger.log(fmt.Sprintf("Unable to read %v.", path.Join(rootPath, relPath)))
		return
	}

	for _, file := range files {
		results <- file
	}

	return
}

type ListResult struct {
	folder string
	result chan os.FileInfo
}

func listWorker(results chan ListResult, rootPath string, folders chan string) {
	defer close(results)

	for folder := range folders {
		result := make(chan os.FileInfo, FOLDER_ENTRY_READ_AHEAD)
		results <- ListResult{folder, result}
		listInto(result, rootPath, folder)
	}
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type ListReader struct {
	results       ListResult
	info          os.FileInfo
	didAdvance    bool
	stats         *Stats
	resultCounter *int64
}

func (lr *ListReader) advance() {
	lr.info, lr.didAdvance = <-lr.results.result

	if !lr.didAdvance {
		return
	}

	*lr.resultCounter += 1

	// Only show progress when A advances, it's less confusing (the same file won't show up twice in the output etc).
	if lr.resultCounter != &lr.stats.aCount {
		return
	}

	logger.progress("%10d: %v", lr.stats.aCount+lr.stats.aMissing, path.Join(lr.results.folder, lr.info.Name()))
}

func comparePaths(aRoot string, bRoot string) {
	var needDataCompare []NameAndSize
	var stats Stats
	var folderWorkOverflow []string

	waitingForFolders := 0

	listWorkA, listWorkB := make(chan string, FOLDER_LISTING_PIPELINE_DEPTH), make(chan string, FOLDER_LISTING_PIPELINE_DEPTH)
	listResultsA, listResultsB := make(chan ListResult, FOLDER_LISTING_PIPELINE_DEPTH), make(chan ListResult, FOLDER_LISTING_PIPELINE_DEPTH)

	enqueueListFolder := func(folder string) {
		waitingForFolders++
		if len(listWorkA) == cap(listWorkA) || len(listWorkB) == cap(listWorkB) {
			folderWorkOverflow = append(folderWorkOverflow, folder)
			return
		}
		listWorkA <- folder
		listWorkB <- folder
	}

	drainOverflow := func() {
		availableSpace := min(cap(listWorkA)-len(listWorkA), cap(listWorkB)-len(listWorkB))
		if availableSpace == 0 {
			return
		}
		drainAmount := min(len(folderWorkOverflow), availableSpace)
		for i := 0; i < drainAmount; i++ {
			folder := folderWorkOverflow[i]
			listWorkA <- folder
			listWorkB <- folder
		}
		folderWorkOverflow = folderWorkOverflow[drainAmount:]
	}

	enqueueListFolder(".")

	// We probably don't want more than 2 workers since that'd make our disk read pattern more random as different
	// workers work on different folders at the same time. But 2 is a good number since source and destination will
	// often be different disks.
	go listWorker(listResultsA, aRoot, listWorkA)
	go listWorker(listResultsB, bRoot, listWorkB)

	lA := ListReader{stats: &stats, resultCounter: &stats.aCount}
	lB := ListReader{stats: &stats, resultCounter: &stats.bCount}

	logger.log("### Finding files...")
	for waitingForFolders > 0 {
		lA.results = <-listResultsA
		lB.results = <-listResultsB
		waitingForFolders--
		drainOverflow()

		if lA.results.folder != lB.results.folder {
			panic("List workers out of sync.")
		}

		lA.advance()
		lB.advance()
		for {
			for lA.didAdvance && (!lB.didAdvance || lA.info.Name() < lB.info.Name()) {
				logger.writeOut(fmt.Sprintf("%v missing.", path.Join(bRoot, lA.results.folder, lA.info.Name())))
				stats.bMissing++
				lA.advance()
			}

			for lB.didAdvance && (!lA.didAdvance || lA.info.Name() > lB.info.Name()) {
				logger.writeOut(fmt.Sprintf("%v missing.", path.Join(aRoot, lA.results.folder, lB.info.Name())))
				stats.aMissing++
				lB.advance()
			}

			if !lA.didAdvance && !lB.didAdvance {
				break
			}

			if !lA.didAdvance || !lB.didAdvance || lA.info.Name() != lB.info.Name() {
				continue
			}

			switch {
			case lA.info.IsDir() && !lB.info.IsDir():
				logger.writeOut(fmt.Sprintf("%v is a directory but counterpart is not.", path.Join(aRoot, lA.results.folder, lA.info.Name())))
			case lB.info.IsDir() && !lA.info.IsDir():
				logger.writeOut(fmt.Sprintf("%v is a directory but counterpart is not.", path.Join(bRoot, lA.results.folder, lB.info.Name())))
			case lA.info.Mode()&os.ModeSymlink != 0 && lB.info.Mode()&os.ModeSymlink == 0:
				logger.writeOut(fmt.Sprintf("%v is a symlink but counterpart is not.", path.Join(aRoot, lA.results.folder, lA.info.Name())))
			case lB.info.Mode()&os.ModeSymlink != 0 && lA.info.Mode()&os.ModeSymlink == 0:
				logger.writeOut(fmt.Sprintf("%v is a symlink but counterpart is not.", path.Join(bRoot, lA.results.folder, lB.info.Name())))
			case lA.info.Mode()&os.ModeSymlink != 0:
				aLink, aErr := os.Readlink(path.Join(aRoot, lA.results.folder, lA.info.Name()))
				if aErr != nil {
					logger.error(fmt.Sprintf("Unable to read symlink of %v.", path.Join(aRoot, lA.results.folder, lA.info.Name())))
				} else {
					bLink, bErr := os.Readlink(path.Join(aRoot, lA.results.folder, lA.info.Name()))
					if bErr != nil {
						logger.error(fmt.Sprintf("Unable to read symlink of %v.", path.Join(bRoot, lA.results.folder, lB.info.Name())))
					} else if aLink != bLink {
						logger.writeOut(fmt.Sprintf("%v and counterpart link to different files: %v and %v.", path.Join(aRoot, lA.results.folder, lB.info.Name()), aLink, bLink))
					}
				}
			case lA.info.IsDir():
				// Go depth first into folders.
				enqueueListFolder(path.Join(lA.results.folder, lA.info.Name()))
			case lA.info.Size() != lB.info.Size():
				logger.writeOut(fmt.Sprintf("%v is %d bytes but counterpart is %d bytes (%d byte(s) difference).", path.Join(aRoot, lA.results.folder, lA.info.Name()), lA.info.Size(), lB.info.Size(), lB.info.Size()-lA.info.Size()))

			default:
				// This name requires a byte by byte comparison.
				needDataCompare = append(needDataCompare, NameAndSize{path.Join(lA.results.folder, lA.info.Name()), lA.info.Size()})
			}

			lA.advance()
			lB.advance()
		}
	}
	close(listWorkA)
	close(listWorkB)

	compareData(&logger, &stats, aRoot, bRoot, needDataCompare)

	logger.log(strings.Repeat("-", 80))
	checkedPercentage := 1.0
	if stats.completeCheck > 0 {
		logger.log(fmt.Sprintf("%d file pair(s) fully equal.", stats.completeCheck))
	}
	if stats.partiallyCheckedSize > 0 {
		checkedPercentage = float64(stats.partiallyCheckedBytes) / float64(stats.partiallyCheckedSize)
		logger.log(fmt.Sprintf("%d file pair(s) at least partially equal. %d/%d bytes (%.2f%%) checked.", stats.partiallyCheckedCount, stats.partiallyCheckedBytes, stats.partiallyCheckedSize, checkedPercentage))
	}
	if stats.unreadable > 0 {
		logger.log(fmt.Sprintf("%d file pair(s) could not be compared.", stats.unreadable))
	}
	if stats.aMissing > 0 {
		logger.log(fmt.Sprintf("At least %d missing file(s) in %v.", stats.aMissing, aRoot))
	}
	if stats.bMissing > 0 {
		logger.log(fmt.Sprintf("At least %d missing file(s) in %v.", stats.bMissing, bRoot))
	}
	if stats.notEqual > 0 {
		logger.log(fmt.Sprintf("At least %d file pair(s) have byte level differences.", stats.notEqual))
	}
}

type ReadBlockResult struct {
	index  int
	offset int64
	block  []byte
	err    error
}

func mustRead(bytesRead chan ReadBlockResult, index int, full_path string, f io.ReadSeeker, offset int64, size int64) bool {
	// fmt.Printf("\nmustRead %v:%v", index, offset)
	_, err := f.Seek(offset, 0)
	if err != nil {
		logger.error(fmt.Sprintf("Unable to read %v (%v).", full_path, err))
		bytesRead <- ReadBlockResult{index: index, err: err}
		return false
	}

	buf := make([]byte, size)
	n, err := io.ReadAtLeast(f, buf, int(minInt64(BLOCK_SIZE, size)))
	if err != nil {
		logger.error(fmt.Sprintf("Unable to read %v (%v).", full_path, err))
		bytesRead <- ReadBlockResult{index: index, err: err}
		return false
	}
	if int(size) != n {
		logger.error(fmt.Sprintf("Unable to read %v (unexpected end of file).", full_path))
		bytesRead <- ReadBlockResult{index: index, err: io.ErrUnexpectedEOF}
		return false
	}

	bytesRead <- ReadBlockResult{index: index, offset: offset, block: buf[:]}
	return true
}

type CancellationIndex struct {
	cancellations map[int]bool
	mutex         sync.Mutex
}

func (c *CancellationIndex) cancel(index int) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.cancellations[index] = true
}

func (c *CancellationIndex) isCancelled(index int) bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.cancellations[index]
}

func readWorker(bytesRead chan ReadBlockResult, root_path string, work []NameAndSize, cancellations *CancellationIndex) {
	defer close(bytesRead)

	for i := 0; i < len(work); i++ {
		if cancellations.isCancelled(i) {
			continue
		}

		if work[i].size == 0 {
			bytesRead <- ReadBlockResult{index: i, block: make([]byte, 0)}
			continue
		}

		full_path := path.Join(root_path, work[i].name)
		// fmt.Printf("\nreading %v", i)

		f, err := os.Open(full_path)
		if err != nil {
			logger.error(fmt.Sprintf("Unable to read %v (%v).", full_path, err))
			bytesRead <- ReadBlockResult{index: i, err: err}
			cancellations.cancel(i)
			f.Close()
			continue
		}

		// Get the first block.
		firstLength := minInt64(BLOCK_SIZE, work[i].size)
		if !mustRead(bytesRead, i, full_path, f, 0, firstLength) {
			cancellations.cancel(i)
			f.Close()
			continue
		}

		// If that was the whole file we're done with this index.
		if firstLength == work[i].size {
			f.Close()
			continue
		}

		// Don't read the second block if the work was cancelled.
		if cancellations.isCancelled(i) {
			f.Close()
			continue
		}

		lastLength := minInt64(work[i].size-firstLength, BLOCK_SIZE)
		if !mustRead(bytesRead, i, full_path, f, work[i].size-lastLength, lastLength) {
			cancellations.cancel(i)
			f.Close()
			continue
		}

		f.Close()
	}

}

func compareData(logger *Logger, stats *Stats, aRoot string, bRoot string, needDataCompare []NameAndSize) {
	logger.log("### Comparing files...")

	cancellations := CancellationIndex{cancellations: make(map[int]bool)}

	aResults, bResults := make(chan ReadBlockResult, DATA_DIFF_PIPELINE_DEPTH), make(chan ReadBlockResult, DATA_DIFF_PIPELINE_DEPTH)
	go readWorker(aResults, aRoot, needDataCompare, &cancellations)
	go readWorker(bResults, bRoot, needDataCompare, &cancellations)

	var aResult, bResult ReadBlockResult
	var aOk, bOk bool
	aResult.index = -1
	bResult.index = -1
	for i := 0; i < len(needDataCompare); i++ {
		var checked int64
		failed := false
		for {
			for ; aResult.index < i; aResult = <-aResults {
			}

			if aResult.err != nil {
				failed = true
				break
			}

			for ; bResult.index < i; bResult = <-bResults {
			}

			if bResult.err != nil {
				failed = true
				break
			}

			// fmt.Printf("\nprocessing block from %v", i)

			if !bytes.Equal(aResult.block, bResult.block) {
				// Just in case any of the workers has not yet gone to the next block, try to save that
				// time. This will play a bigger role in the future with an option to diff more than 2
				// blocks.
				cancellations.cancel(i)

				// TODO offset + location of diff == byte position of difference
				if aResult.offset == 0 {
					logger.writeOut(fmt.Sprintf("%v and counterpart differ on first block.", path.Join(aRoot, needDataCompare[i].name)))
				} else {
					logger.writeOut(fmt.Sprintf("%v and counterpart differ on last block.", path.Join(aRoot, needDataCompare[i].name)))
				}
				stats.notEqual++
				break
			}

			checked += int64(len(aResult.block))
			aResult, aOk = <-aResults
			bResult, bOk = <-bResults

			// Out of blocks to compare in this index?
			if !aOk || !bOk || aResult.index > i || bResult.index > i {
				break
			}
		}

		if failed {
			stats.unreadable++
		} else if checked == needDataCompare[i].size {
			stats.completeCheck++
		} else {
			stats.partiallyCheckedCount++
			stats.partiallyCheckedBytes += checked
			stats.partiallyCheckedSize += needDataCompare[i].size
		}

		stats.totalSize += needDataCompare[i].size
		logger.progress("%10d/%10d: %v", i, len(needDataCompare), needDataCompare[i].name)
	}
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `
Usage: %s [OPTION]... FILE FILE
Recursively compare the paths given by FILEs and try to quickly find differences between the two. The focus is on finding obvious differences fast rather than doing a full byte by byte comparison.

`,
			os.Args[0])
		flag.PrintDefaults()
	}

	var quiet = flag.BoolP("quiet", "q", false, "Suppress progress and final summary message; only print differences found")

	flag.Parse()

	if len(flag.Args()) != 2 {
		flag.Usage()
		os.Exit(1)
	}

	aRoot := flag.Args()[0]
	bRoot := flag.Args()[1]

	if aRoot > bRoot {
		aRoot, bRoot = bRoot, aRoot
	}

	if false {
		defer profile.Start().Stop()
	}

	quitLogger := make(chan int)
	logger = Logger{quiet: *quiet}
	go logger.run(quitLogger)
	comparePaths(aRoot, bRoot)
	close(quitLogger)
}
