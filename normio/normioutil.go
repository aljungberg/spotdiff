package normioutil

import (
	"golang.org/x/text/unicode/norm"
	"os"
	"sort"
)

//
// tl;dr: ioutil.ReadDir with normalised UTF
//
// Since there are multiple encodings to express the same string value in UTF we need to normalise the filenames
// before sorting or comparing them. For example:
//
// Filesystem A has a file named "Américo" encoded as 41 6d 65 cc 81 72 69 63 6f.
// Filesystem B has a file named "Américo" encoded as 41 6d c3 a9 72 69 63 6f.
//
// These filenames are in fact equal. They only differ in how the value is encoded. We normalise them to
// NFC form and voila, they will sort into the same slot in our filename list and compare equal with ==.
//

type NormFileInfo struct {
	os.FileInfo
}

func (fi *NormFileInfo) Name() string {
	return norm.NFC.String(fi.FileInfo.Name())
}

func (fi *NormFileInfo) FileName() string {
	// Return the file name without normalisation.
	return fi.FileInfo.Name()
}

type byName []NormFileInfo

func (f byName) Len() int           { return len(f) }
func (f byName) Less(i, j int) bool { return f[i].Name() < f[j].Name() }
func (f byName) Swap(i, j int)      { f[i], f[j] = f[j], f[i] }

// ReadDir reads the directory named by dirname and returns
// a list of directory entries sorted by filename.
func ReadDir(dirname string) ([]NormFileInfo, error) {
	f, err := os.Open(dirname)
	if err != nil {
		return nil, err
	}
	unwrapped, err := f.Readdir(-1)
	f.Close()
	if err != nil {
		return nil, err
	}

	// TODO Could reimplement at the os/file_unix level to make NormFileInfo from the start and
	// get rid of the temporary list, save some memory. Probably not worth the extra complexity
	// though.
	var wrapped = make([]NormFileInfo, len(unwrapped))
	for i, entry := range unwrapped {
		wrapped[i] = NormFileInfo{entry}
	}

	sort.Sort(byName(wrapped))
	return wrapped, nil
}
