package connector

import (
	"errors"
	"net"
	"strconv"
)

func splitTarget(s string) (string, int, error) {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return "", 0, errors.New("target must be host:port")
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return "", 0, errors.New("invalid target port")
	}
	return host, p, nil
}
