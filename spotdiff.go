package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/aljungberg/spotdiff/normio"
	tm "github.com/buger/goterm"
	"github.com/pkg/profile"
	flag "github.com/spf13/pflag"
	"io"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync"
	"time"
)

type NameAndSize struct {
	name    string
	size    int64
	checked int64
	differs bool
	failed  bool
}

type Stats struct {
	aCount   int64
	bCount   int64
	aMissing int64
	bMissing int64

	totalPairSize                       int64
	totalPairCount                      int
	failedPairSize                      int64
	failedPairCount                     int
	differingPairSize                   int64
	differingPairCount                  int
	fullyCheckedPairSize                int64
	fullyCheckedPairCount               int
	partiallyCheckedPairSize            int64
	partiallyCheckedPairActuallyChecked int64
	partiallyCheckedPairCount           int

	wasCancelled bool
}

const VERSION = "0.9.2"

const BLOCK_SIZE int64 = 16 * 1024
const FOLDER_LISTING_PIPELINE_DEPTH = 16
const FOLDER_ENTRY_READ_AHEAD = 64
const DATA_DIFF_PIPELINE_DEPTH = 128
const PROGRESS_FREQUENCY = 200 * time.Millisecond
const DEBUG_UTF = false

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
	// FIXME The reset clears the stderr line but then we write to the stdout one, so don't use this.
	// l.resetIfNeeded()
	// TODO Don't write control codes to stdout if it's not a tty.
	os.Stdout.WriteString("\r\033[K" + text + "\n")
}

// Write an informative message to stderr. The message will be suppressed in quiet mode.
func (l *Logger) log(text string) {
	if l.quiet {
		return
	}

	l.resetIfNeeded()
	os.Stderr.WriteString(text + "\n")
}

func headlinePad(text string, l int) string {
	return "- " + text + " " + strings.Repeat("-", max(0, l-len(text)-4))
}

func listInto(results chan normioutil.NormFileInfo, rootPath string, relPath string, filter pathFilter) {
	defer close(results)

	// It's important that the results are sorted.
	files, err := normioutil.ReadDir(path.Join(rootPath, relPath))

	if err != nil {
		logger.log(fmt.Sprintf("Unable to read %v (%v).", path.Join(rootPath, relPath), err))
		return
	}

	for _, file := range files {
		if !filter.includes(path.Join(relPath, file.Name())) {
			continue
		}
		results <- file
	}

	return
}

type ListResult struct {
	folder string
	result chan normioutil.NormFileInfo
}

func listWorker(results chan ListResult, rootPath string, folders chan string, filter pathFilter) {
	defer close(results)

	for folder := range folders {
		result := make(chan normioutil.NormFileInfo, FOLDER_ENTRY_READ_AHEAD)
		results <- ListResult{folder, result}
		listInto(result, rootPath, folder, filter)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
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
	info          normioutil.NormFileInfo
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

type pathFilterRule struct {
	pattern  string
	isAccept bool
}

func (r *pathFilterRule) matches(relativePath string) bool {
	// Full path match when the pattern starts with /.
	if strings.HasPrefix(r.pattern, "/") && r.pattern[1:] == relativePath {
		return true
	}

	// Last element only match.
	var base = path.Base(relativePath)
	return r.pattern == base
}

type pathFilter struct {
	rules []pathFilterRule
}

func (pf *pathFilter) addExclude(pattern string) {
	pf.rules = append(pf.rules, pathFilterRule{pattern, false})
}

func (pf *pathFilter) includes(relativePath string) bool {
	for _, rule := range pf.rules {
		if rule.matches(relativePath) {
			return rule.isAccept
		}
	}
	return true
}

func allOfZeroPercent(a int64, b int64) float64 {
	if b == 0 {
		return 1.0
	}
	return float64(a) / float64(b)
}

func comparePaths(aRoot string, bRoot string, skipFull bool, filter pathFilter) {
	var needDataCompare []NameAndSize
	var stats Stats
	var folderWorkOverflow []string

	waitingForFolders := 0

	listWorkA, listWorkB := make(chan string, FOLDER_LISTING_PIPELINE_DEPTH), make(chan string, FOLDER_LISTING_PIPELINE_DEPTH)
	listResultsA, listResultsB := make(chan ListResult, FOLDER_LISTING_PIPELINE_DEPTH), make(chan ListResult, FOLDER_LISTING_PIPELINE_DEPTH)

	enqueueListFolder := func(folder string) {
		if !filter.includes(folder) {
			return
		}
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
	go listWorker(listResultsA, aRoot, listWorkA, filter)
	go listWorker(listResultsB, bRoot, listWorkB, filter)

	lA := ListReader{stats: &stats, resultCounter: &stats.aCount}
	lB := ListReader{stats: &stats, resultCounter: &stats.bCount}

	logger.log(headlinePad("Finding files", 80))
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
				if DEBUG_UTF && lB.didAdvance {
					logger.writeOut(fmt.Sprintf("%v != %v", hex.Dump([]byte(lA.info.Name())), hex.Dump([]byte(lB.info.Name()))))
				}
				logger.writeOut(fmt.Sprintf("%v missing.", path.Join(bRoot, lA.results.folder, lA.info.Name())))
				stats.bMissing++
				lA.advance()
			}

			for lB.didAdvance && (!lA.didAdvance || lA.info.Name() > lB.info.Name()) {
				if DEBUG_UTF && lA.didAdvance {
					logger.writeOut(fmt.Sprintf("%v != %v", hex.Dump([]byte(lA.info.Name())), hex.Dump([]byte(lB.info.Name()))))
				}
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
				aLink, aErr := os.Readlink(path.Join(aRoot, lA.results.folder, lA.info.FileName()))
				if aErr != nil {
					logger.error(fmt.Sprintf("Unable to read symlink of %v.", path.Join(aRoot, lA.results.folder, lA.info.Name())))
				} else {
					bLink, bErr := os.Readlink(path.Join(aRoot, lA.results.folder, lA.info.FileName()))
					if bErr != nil {
						logger.error(fmt.Sprintf("Unable to read symlink of %v.", path.Join(bRoot, lA.results.folder, lB.info.Name())))
					} else if aLink != bLink {
						logger.writeOut(fmt.Sprintf("%v and counterpart link to different files: %v and %v.", path.Join(aRoot, lA.results.folder, lB.info.Name()), aLink, bLink))
					}
				}
			case lA.info.IsDir():
				// We do folders breadth-first, so we can just enqueue the subfolder at the end of the queue.
				enqueueListFolder(path.Join(lA.results.folder, lA.info.FileName()))
			case lA.info.Size() != lB.info.Size():
				logger.writeOut(fmt.Sprintf("%v is %d bytes but counterpart is %d bytes (%d byte(s) difference).", path.Join(aRoot, lA.results.folder, lA.info.Name()), lA.info.Size(), lB.info.Size(), lB.info.Size()-lA.info.Size()))

			default:
				// This name requires a byte by byte comparison.
				stats.totalPairSize += lA.info.Size()
				stats.totalPairCount++
				needDataCompare = append(needDataCompare, NameAndSize{name: path.Join(lA.results.folder, lA.info.FileName()), size: lA.info.Size()})
			}

			lA.advance()
			lB.advance()
		}
	}
	close(listWorkA)
	close(listWorkB)

	if len(needDataCompare) != stats.totalPairCount {
		panic("len(needDataCompare) != stats.totalPairCount")
	}

	compareData(&logger, &stats, aRoot, bRoot, needDataCompare, skipFull)

	logger.log(headlinePad("Result", 80))
	if stats.wasCancelled {
		logger.log("Interrupted before completion.")
	}

	for i := 0; i < len(needDataCompare); i++ {
		if needDataCompare[i].differs {
			stats.differingPairSize += needDataCompare[i].size
			stats.differingPairCount++
			continue
		}
		if needDataCompare[i].failed {
			stats.failedPairSize += needDataCompare[i].size
			stats.failedPairCount++
			continue
		}

		if !stats.wasCancelled && !skipFull && needDataCompare[i].checked != needDataCompare[i].size {
			panic(fmt.Sprintf("Failed to full check %s: %d/%d bytes checked.", needDataCompare[i].name, needDataCompare[i].checked, needDataCompare[i].size))
		}

		if needDataCompare[i].checked == needDataCompare[i].size {
			stats.fullyCheckedPairSize += needDataCompare[i].size
			stats.fullyCheckedPairCount++
		} else {
			stats.partiallyCheckedPairSize += needDataCompare[i].size
			stats.partiallyCheckedPairActuallyChecked += needDataCompare[i].checked
			stats.partiallyCheckedPairCount++
		}
	}

	if stats.differingPairCount+stats.failedPairCount+stats.fullyCheckedPairCount+stats.partiallyCheckedPairCount != stats.totalPairCount {
		panic("Incorrect stats.")
	}

	logger.log(fmt.Sprintf("          Total: %d file pairs (%d bytes)", stats.totalPairCount, stats.totalPairSize))
	logger.log(fmt.Sprintf("    Fully equal: %d file pairs (%d bytes, %.2f%%)", stats.fullyCheckedPairCount, stats.fullyCheckedPairSize, 100*allOfZeroPercent(stats.fullyCheckedPairSize, stats.totalPairSize)))
	logger.log(fmt.Sprintf("Partially equal: %d file pairs (%d bytes, %.2f%%)", stats.partiallyCheckedPairCount, stats.partiallyCheckedPairSize, 100*allOfZeroPercent(stats.partiallyCheckedPairSize, stats.totalPairSize)))
	if stats.partiallyCheckedPairActuallyChecked > 0 {
		logger.log(fmt.Sprintf("(Of the partially checked files, %d bytes or %.2f%% were tested.)", stats.partiallyCheckedPairActuallyChecked, 100*allOfZeroPercent(stats.partiallyCheckedPairActuallyChecked, stats.partiallyCheckedPairSize)))
	}
	if stats.differingPairCount == 0 && stats.failedPairSize == 0 && stats.aMissing == 0 && stats.bMissing == 0 {
		return
	}

	logger.log("")
	logger.log(fmt.Sprintf("      Differing: %d file pairs (%d bytes, %.2f%%)", stats.differingPairCount, stats.differingPairSize, 100*allOfZeroPercent(stats.differingPairSize, stats.totalPairSize)))
	logger.log(fmt.Sprintf("     Unreadable: %d file pairs (%d bytes, %.2f%%)", stats.failedPairCount, stats.failedPairSize, 100*allOfZeroPercent(stats.failedPairSize, stats.totalPairSize)))

	if stats.aMissing > 0 {
		logger.log(fmt.Sprintf("   Missing left: at least %d missing file(s) in %v.", stats.aMissing, aRoot))
	}
	if stats.bMissing > 0 {
		logger.log(fmt.Sprintf("  Missing right: at least %d missing file(s) in %v.", stats.bMissing, bRoot))
	}

}

type ReadBlockResult struct {
	index  int
	offset int64
	block  []byte
	err    error
}

func mustRead(bytesRead chan ReadBlockResult, index int, full_path string, f io.ReadSeeker, offset int64, lastOffset *int64, size int64) bool {
	// fmt.Printf("\nmustRead %v:%v", index, offset)
	if offset != *lastOffset {
		_, err := f.Seek(offset, 0)
		if err != nil {
			logger.error(fmt.Sprintf("Unable to read %v (%v).", full_path, err))
			bytesRead <- ReadBlockResult{index: index, err: err}
			return false
		}
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

	*lastOffset = offset + size
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

func readWorker(bytesRead chan ReadBlockResult, root_path string, work []NameAndSize, cancellations *CancellationIndex, full bool) {
	defer close(bytesRead)

	for i := 0; i < len(work); i++ {
		if cancellations.isCancelled(i) {
			continue
		}

		if work[i].size == 0 {
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

		var lastOffset int64 = 0

		if !full {
			// Get the first block.
			firstLength := minInt64(BLOCK_SIZE, work[i].size)
			if !mustRead(bytesRead, i, full_path, f, 0, &lastOffset, firstLength) {
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
			if !mustRead(bytesRead, i, full_path, f, work[i].size-lastLength, &lastOffset, lastLength) {
				cancellations.cancel(i)
				f.Close()
				continue
			}
		} else {
			// Read all the blocks except for the first and the last.
			if work[i].size <= 2*BLOCK_SIZE {
				// We already compared all of the blocks.
				f.Close()
				continue
			}

			// fmt.Printf("\nReading %d: %s of %d bytes", i, work[i].name, work[i].size)
			for offset := BLOCK_SIZE; offset < work[i].size-BLOCK_SIZE; offset += BLOCK_SIZE {
				readTo := minInt64(offset+BLOCK_SIZE, work[i].size-BLOCK_SIZE)
				if !mustRead(bytesRead, i, full_path, f, offset, &lastOffset, readTo-offset) {
					cancellations.cancel(i)
					break
				}
			}
		}

		f.Close()
	}

}

// (Slow) method to find the first differing bytes in the given array.
func indexOfDifference(blockA []byte, blockB []byte) int {
	l := min(len(blockA), len(blockB))

	for i := 0; i < l; i++ {
		if blockA[i] != blockB[i] {
			return i
		}
	}

	return -1
}

func compareData(logger *Logger, stats *Stats, aRoot string, bRoot string, needDataCompare []NameAndSize, skipFull bool) {
	cancelChannel := make(chan os.Signal, 1)
	signal.Notify(cancelChannel, os.Interrupt)
	go func() {
		<-cancelChannel
		stats.wasCancelled = true
	}()

	cancellations := CancellationIndex{cancellations: make(map[int]bool)}

	// It's possible to cancel this part too but we only advertise it for the last stage.
	logger.log(headlinePad("Quick comparing files", 80))

	aResults, bResults := make(chan ReadBlockResult, DATA_DIFF_PIPELINE_DEPTH), make(chan ReadBlockResult, DATA_DIFF_PIPELINE_DEPTH)
	go readWorker(aResults, aRoot, needDataCompare, &cancellations, false)
	go readWorker(bResults, bRoot, needDataCompare, &cancellations, false)
	stageCompareData(logger, stats, aRoot, bRoot, needDataCompare, &cancellations, aResults, bResults)

	if skipFull || stats.wasCancelled {
		return
	}

	logger.log(headlinePad("Fully comparing files (Ctrl-C to cancel)", 80))

	aResultsFull, bResultsFull := make(chan ReadBlockResult, DATA_DIFF_PIPELINE_DEPTH), make(chan ReadBlockResult, DATA_DIFF_PIPELINE_DEPTH)
	go readWorker(aResultsFull, aRoot, needDataCompare, &cancellations, true)
	go readWorker(bResultsFull, bRoot, needDataCompare, &cancellations, true)
	stageCompareData(logger, stats, aRoot, bRoot, needDataCompare, &cancellations, aResultsFull, bResultsFull)
}

func stageCompareData(logger *Logger, stats *Stats, aRoot string, bRoot string, needDataCompare []NameAndSize, cancellations *CancellationIndex, aResults chan ReadBlockResult, bResults chan ReadBlockResult) {
	var aResult, bResult ReadBlockResult
	var aOk, bOk bool = true, true
	aResult.index = -1
	bResult.index = -1

needDataCompareLoop:
	for i := 0; i < len(needDataCompare); i++ {
		if stats.wasCancelled {
			return
		}
		if needDataCompare[i].failed {
			continue
		}

		for {
			for ; aResult.index < i && aOk; aResult, aOk = <-aResults {
			}

			if i == aResult.index && aResult.err != nil {
				if !needDataCompare[i].failed {
					needDataCompare[i].failed = true
				}
				break
			}

			for ; bResult.index < i && bOk; bResult, bOk = <-bResults {
			}

			if i == bResult.index && bResult.err != nil {
				if !needDataCompare[i].failed {
					needDataCompare[i].failed = true
				}
				break
			}

			if !aOk || aResult.index > i {
				continue needDataCompareLoop
			}

			if !bOk || bResult.index > i {
				continue needDataCompareLoop
			}

			// fmt.Printf("\nprocessing block from %v\n", i)

			if !bytes.Equal(aResult.block, bResult.block) {
				// Just in case any of the workers has not yet gone to the next block, try to save that
				// time. This will play a bigger role in the future with an option to diff more than 2
				// blocks.
				cancellations.cancel(i)

				needDataCompare[i].differs = true

				logger.writeOut(fmt.Sprintf("%v and counterpart differ on byte %d.", path.Join(aRoot, needDataCompare[i].name), aResult.offset+int64(indexOfDifference(aResult.block, bResult.block))))
				break
			}

			needDataCompare[i].checked += int64(len(aResult.block))

			if needDataCompare[i].checked > needDataCompare[i].size {
				panic(fmt.Sprintf("%d: Compared more data (%d) than there are bytes in %s (%d)!", i, needDataCompare[i].checked, needDataCompare[i].name, needDataCompare[i].size))
			}

			var fileProgress float64
			if needDataCompare[i].size == 0 {
				fileProgress = 1
			} else {
				fileProgress = float64(needDataCompare[i].checked) / float64(needDataCompare[i].size)
			}
			logger.progress("%10d/%10d: (%6.2f%%) %v", i, len(needDataCompare), 100*fileProgress, needDataCompare[i].name)

			if stats.wasCancelled {
				return
			}

			aResult, aOk = <-aResults
			bResult, bOk = <-bResults
		}
	}
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `
Usage: %s [OPTION]... FILE FILE

Compare two folders, fast.

Recursively compare the paths given by FILEs and try to quickly find differences between the two. Fastdiff finds obvious
differences quickly, and reports them immediately, then keeps looking. The longer spotdiff runs, the smaller the
chance that any unreported differences remain. This allows a user who is fairly confident there are no differences to stop
the search early, whereas if the tool is run to completion a full byte-for-byte comparison will be performed.

`,
			os.Args[0])
		flag.PrintDefaults()
	}

	var skipFull = flag.Bool("skip-full", false, fmt.Sprintf("only compare the first and last %d bytes of each file; stop immediately after the quick compare stage", BLOCK_SIZE))
	var printVersion = flag.Bool("version", false, "output version information and exit")
	var quiet = flag.BoolP("quiet", "q", false, "suppress progress and final summary message; only print differences found")
	var excludes = flag.StringArrayP("exclude", "", nil, "exclude files matching the given pattern")
	flag.Parse()

	if *printVersion {
		fmt.Printf("spotdiff version %s\n", VERSION)
		os.Exit(0)
	}

	if len(flag.Args()) != 2 {
		flag.Usage()
		os.Exit(1)
	}

	aRoot := flag.Args()[0]
	bRoot := flag.Args()[1]

	if aRoot > bRoot {
		aRoot, bRoot = bRoot, aRoot
	}

	var filter = pathFilter{}
	for _, excludePattern := range *excludes {
		filter.addExclude(excludePattern)
	}

	if false {
		defer profile.Start().Stop()
	}

	quitLogger := make(chan int)
	logger = Logger{quiet: *quiet}
	go logger.run(quitLogger)
	comparePaths(aRoot, bRoot, *skipFull, filter)
	close(quitLogger)
}
