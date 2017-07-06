# vim: set ft=make ffs=unix fenc=utf8:
# vim: set noet ts=4 sw=4 tw=72 list:
#
all: freebsd linux man

freebsd:
	@env GOOS=freebsd GOARCH=amd64 go install -ldflags "-X main.zkonceVersion=`git rev-parse --short HEAD`"

linux:
	@env GOOS=linux GOARCH=amd64 go install -ldflags "-X main.zkonceVersion=`git rev-parse --short HEAD`"

man: freebsd linux
	@${GOPATH}/bin/zkonce --create-manpage > zkonce.1
