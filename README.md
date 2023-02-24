# spotdiff: The faster way to spot differences between folders

Spotdiff scans your folders and reports differences immediately, then digs deeper to find even the smallest changes. Stop it anytime you're confident you've seen what you need, or let it go the distance for a full byte-by-byte comparison.

Spotdiff is faster than other comparison tools like `diff -r` or `rsync --dry-run` because it doesn't work sequentially. Spotdiff uses a progressive search strategy that prioritizes breadth over depth first.

Use spotdiff to quickly find missing files or corrupted copies, saving you time and ensuring that your data is safe.

A key feature of spotdiff is its progressive nature. The tool will try to spot clear differences as early as possible in the search, allowing you to cancel the search when you've seen what you need to see. In a sense, we trade throughput for latency. Skipping around like spotdiff does might take slightly more total time than a sequential byte-by-byte diff, but the "time to first difference found" is much shorter on average.

Spotdiff performs multiple passes over the set of files, each pass deeper. This is key when comparing very large folders. If comparing everything is going to take 3 days, you want a tool like spotdiff so you might have initial reports of differences after just a few minutes. When a whole subfolder or file is missing, you'll know it within moments. A more subtle difference like a single byte in the beginning of a file you'll know "soon". A very well hidden difference like a byte in a random spot in a huge file, you'll know "eventually". The longer spotdiff runs, the smaller the chance that any unreported differences remain. 

Use spotdiff to answer questions like:

- Do I need to re-sync this backup copy, is it up to date?
- Is this folder an accidental duplicate of this other folder?
- Has this copy been corrupted (bit rotted) on disk?

Personally I use it to verify my backups.

The search strategy is as follows:

- Pass 1: Breadth-first directory scan to find missing files and subfolders, prioritizing differences closer to the root of the tree.
- Pass 2: Compare the first and last 16 kB of every file for an initial "quick comparison", identifying obvious problems like zeroed or garbled files.
- Pass 3: Perform a full comparison of the remaining bytes of every file, excluding the first and trailing 16kB to avoid redundancy.

## Example output

```
$ spotdiff test/a test/b
- Finding files ---------------------------------------------------------------
test/b/a_only.txt missing.
test/a/b_only.txt missing.
test/a/diff_size.txt is 100 bytes but counterpart is 101 bytes (1 byte(s) difference).
test/a/dir_in_1 is a directory but counterpart is not.
test/a/unreadable_a missing.
test/b/recursive/unreadable_b missing.
test/b/recursive_diff/a_only.txt missing.
test/a/recursive_diff/b_only.txt missing.
test/a/recursive_diff/diff_size.txt is 100 bytes but counterpart is 101 bytes (1 byte(s) difference).
test/a/recursive_diff/dir_in_1 is a directory but counterpart is not.
- Quick comparing files -------------------------------------------------------
test/a/diff1.txt and counterpart differ on byte 0.
test/a/diff_last.txt and counterpart differ on byte 18890.
...
test/a/size_8193/diff_on_8191 and counterpart differ on byte 8191.
test/a/size_8193/diff_on_8192 and counterpart differ on byte 8192.
- Fully comparing files (Ctrl-C to cancel) ------------------------------------
test/a/size_32769/diff_on_16384 and counterpart differ on byte 16384.
test/a/size_49152/diff_on_16384 and counterpart differ on byte 16384.
test/a/size_49152/diff_on_16385 and counterpart differ on byte 16385.
test/a/size_49152/diff_on_32767 and counterpart differ on byte 32767.
- Result ----------------------------------------------------------------------
          Total: 90 file pairs (1597695 bytes)
    Fully equal: 5 file pairs (32783 bytes, 2.05%)
Partially equal: 0 file pairs (0 bytes, 0.00%)

      Differing: 85 file pairs (1564912 bytes, 97.95%)
     Unreadable: 0 file pairs (0 bytes, 0.00%)
   Missing left: at least 3 missing file(s) in test/a.
  Missing right: at least 3 missing file(s) in test/b.
```

## Building

```
go build
```

