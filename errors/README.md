# errors 

[![CircleCI](https://circleci.com/gh/cloudengio/go.pkg.svg?style=svg)](https://circleci.com/gh/cloudengio/go.pkg)

errors provides utility routines for working with errors that are compatible with go 1.13+.

It currently provides:

1. `errors.M` which can be used to store multiple error values. `errors.M` is thread safe.

```go
errs := errors.M{}
...
errs.Append(fn(a))
...
errs.Append(fn(b))
...
err := errs.Err()
```