# Contribute

1. [Development Environment](#development-environment)
2. [Vendor](#vendor)


## Development Environment

Clone the repo like this:
```bash
$ cd $GOPATH/src
$ git clone git@github.com:KurokuLabs/margo.git
```
This is essential as the application's imports relies on `margo.sh/cmdpkg/margo`.

## Vendor

You need `dep`. If you don't have it, you can install it by:
```bash
$ go get -u github.com/golang/dep/cmd/dep
```

Now update your vendors by doing:
```bash
$ dep ensure
```
