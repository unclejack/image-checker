image-checker
==============

image-checker can be used to verify whether a Docker image handles signals properly or not. Handling signals properly is essential for proper graceful container stopping.

Installation
------------

```
$ git clone https://github.com/unclejack/image-checker.git
$ cd image-checker
$ go build 
```

Usage
-----

The command line options are:
```
USAGE: image-checker [options] image
  -autocleanup="true" remove the container when running into errors
  -runargs="-d" necessary options for running the container
  -runcmd="" command to run in the container; defaults to entrypoint/cmd
```

An image can be tested like this:
```
$ image-checker pgsql
image-checker results for 'pgsql':
Test graceful stop: PASSED
Test start after stopping: PASSED
Command was specified: NO
```
