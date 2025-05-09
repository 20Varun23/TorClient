package utils

import (
	"bytes"
	"crypto/sha1"
	"net/url"
	"strconv"

	"github.com/jackpal/bencode-go"
)

func MakePstr() ([]byte, error) {
	b := make([]byte, 19)
	a := []byte("BitTorrent protocol")

	copy(b, a)

	return b, nil

}

func SHA1(b *BitTorrent) ([20]byte, error) {
	i := b.Info
	var buf bytes.Buffer
	err := bencode.Marshal(&buf, i)
	if err != nil {
		return [20]byte{}, err
	}
	h := sha1.Sum(buf.Bytes())
	return h, nil
}

func SHA1Bytes(buf []byte) [20]byte {
	h := sha1.Sum(buf)
	return h
}

func AnnounceURL(announce string, sha1 [20]byte, peerID string, port string, L int, down int, up int) (string, error) {
	u, err := url.Parse(announce)
	if err != nil {
		return "", err
	}

	params := url.Values{
		"info_hash":  []string{string(sha1[:])},
		"peer_id":    []string{peerID},
		"port":       []string{port},
		"uploaded":   []string{strconv.Itoa(up)},
		"downloaded": []string{strconv.Itoa(down)},
		"compact":    []string{"1"},
		"left":       []string{strconv.Itoa(L)},
	}
	u.RawQuery = params.Encode()
	return u.String(), nil
}