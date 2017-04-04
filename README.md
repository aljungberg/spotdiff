Recursively compare two paths and try to quickly find differences between the two. 

Fastdiff finds obvious differences quickly, and reports them immediately, but then keeps looking. The longer fastdiff runs, the smaller the
chance that any unreported differences remain. This allows a user who is fairly confident there are no differences to stop
the search early, while still allowing the option to keep running to 100% completion in other situations.

## Usage

```
$ fastdiff test/a test/b
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
test/a/recursive_diff/diff1.txt and counterpart differ on byte 0.
test/a/size_1/diff_on_0 and counterpart differ on byte 0.
test/a/size_12288/diff_on_0 and counterpart differ on byte 0.
test/a/size_12288/diff_on_1 and counterpart differ on byte 1.
test/a/size_12288/diff_on_12287 and counterpart differ on byte 12287.
test/a/size_12288/diff_on_4095 and counterpart differ on byte 4095.
test/a/size_12288/diff_on_4096 and counterpart differ on byte 4096.
test/a/size_12288/diff_on_4097 and counterpart differ on byte 4097.
test/a/size_12288/diff_on_8191 and counterpart differ on byte 8191.
test/a/size_12288/diff_on_8192 and counterpart differ on byte 8192.
test/a/size_12288/diff_on_8193 and counterpart differ on byte 8193.
test/a/size_16383/diff_on_0 and counterpart differ on byte 0.
test/a/size_16383/diff_on_1 and counterpart differ on byte 1.
test/a/size_16383/diff_on_16382 and counterpart differ on byte 16382.
test/a/size_16384/diff_on_0 and counterpart differ on byte 0.
test/a/size_16384/diff_on_1 and counterpart differ on byte 1.
test/a/size_16384/diff_on_16383 and counterpart differ on byte 16383.
test/a/size_16385/diff_on_0 and counterpart differ on byte 0.
test/a/size_16385/diff_on_1 and counterpart differ on byte 1.
test/a/size_16385/diff_on_16383 and counterpart differ on byte 16383.
test/a/size_16385/diff_on_16384 and counterpart differ on byte 16384.
test/a/size_2/diff_on_0 and counterpart differ on byte 0.
test/a/size_2/diff_on_1 and counterpart differ on byte 1.
test/a/size_3/diff_on_0 and counterpart differ on byte 0.
test/a/size_3/diff_on_1 and counterpart differ on byte 1.
test/a/size_3/diff_on_2 and counterpart differ on byte 2.
test/a/size_32767/diff_on_0 and counterpart differ on byte 0.
test/a/size_32767/diff_on_1 and counterpart differ on byte 1.
test/a/size_32767/diff_on_16383 and counterpart differ on byte 16383.
test/a/size_32767/diff_on_16384 and counterpart differ on byte 16384.
test/a/size_32767/diff_on_16385 and counterpart differ on byte 16385.
test/a/size_32767/diff_on_32766 and counterpart differ on byte 32766.
test/a/size_32768/diff_on_0 and counterpart differ on byte 0.
test/a/size_32768/diff_on_1 and counterpart differ on byte 1.
test/a/size_32768/diff_on_16383 and counterpart differ on byte 16383.
test/a/size_32768/diff_on_16384 and counterpart differ on byte 16384.
test/a/size_32768/diff_on_16385 and counterpart differ on byte 16385.
test/a/size_32768/diff_on_32767 and counterpart differ on byte 32767.
test/a/size_32769/diff_on_0 and counterpart differ on byte 0.
test/a/size_32769/diff_on_1 and counterpart differ on byte 1.
test/a/size_32769/diff_on_16383 and counterpart differ on byte 16383.
test/a/size_32769/diff_on_16385 and counterpart differ on byte 16385.
test/a/size_32769/diff_on_32767 and counterpart differ on byte 32767.
test/a/size_32769/diff_on_32768 and counterpart differ on byte 32768.
test/a/size_4095/diff_on_0 and counterpart differ on byte 0.
test/a/size_4095/diff_on_1 and counterpart differ on byte 1.
test/a/size_4095/diff_on_4094 and counterpart differ on byte 4094.
test/a/size_4096/diff_on_0 and counterpart differ on byte 0.
test/a/size_4096/diff_on_1 and counterpart differ on byte 1.
test/a/size_4096/diff_on_4095 and counterpart differ on byte 4095.
test/a/size_4097/diff_on_0 and counterpart differ on byte 0.
test/a/size_4097/diff_on_1 and counterpart differ on byte 1.
test/a/size_4097/diff_on_4095 and counterpart differ on byte 4095.
test/a/size_4097/diff_on_4096 and counterpart differ on byte 4096.
test/a/size_49152/diff_on_0 and counterpart differ on byte 0.
test/a/size_49152/diff_on_1 and counterpart differ on byte 1.
test/a/size_49152/diff_on_16383 and counterpart differ on byte 16383.
test/a/size_49152/diff_on_32768 and counterpart differ on byte 32768.
test/a/size_49152/diff_on_32769 and counterpart differ on byte 32769.
test/a/size_49152/diff_on_49151 and counterpart differ on byte 49151.
test/a/size_8191/diff_on_0 and counterpart differ on byte 0.
test/a/size_8191/diff_on_1 and counterpart differ on byte 1.
test/a/size_8191/diff_on_4095 and counterpart differ on byte 4095.
test/a/size_8191/diff_on_4096 and counterpart differ on byte 4096.
test/a/size_8191/diff_on_4097 and counterpart differ on byte 4097.
test/a/size_8191/diff_on_8190 and counterpart differ on byte 8190.
test/a/size_8192/diff_on_0 and counterpart differ on byte 0.
test/a/size_8192/diff_on_1 and counterpart differ on byte 1.
test/a/size_8192/diff_on_4095 and counterpart differ on byte 4095.
test/a/size_8192/diff_on_4096 and counterpart differ on byte 4096.
test/a/size_8192/diff_on_4097 and counterpart differ on byte 4097.
test/a/size_8192/diff_on_8191 and counterpart differ on byte 8191.
test/a/size_8193/diff_on_0 and counterpart differ on byte 0.
test/a/size_8193/diff_on_1 and counterpart differ on byte 1.
test/a/size_8193/diff_on_4095 and counterpart differ on byte 4095.
test/a/size_8193/diff_on_4096 and counterpart differ on byte 4096.
test/a/size_8193/diff_on_4097 and counterpart differ on byte 4097.
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
export GOPATH=$HOME/golang
go get
go build
```

