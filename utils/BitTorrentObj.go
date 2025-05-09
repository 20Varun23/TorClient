package utils

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"sync"
)

type BitTorrentInfo struct {
	Name         string `bencode:"name"`
	Pieces       string `bencode:"pieces"`
	Length       int    `bencode:"length"`
	Piece_length int    `bencode:"piece length"`
}

type BitTorrent struct {
	Announce string         `bencode:"announce"`
	Info     BitTorrentInfo `bencode:"info"`
}

type Peer struct {
	IP   net.IP
	Port uint16
}

func PeerId() (string, error) {
	PeerIDB := make([]byte, 20)

	charset := "qwertyuiopasdfghjklzxcvbnmQWERTYUIOPASDFGHJKLZXCVBNM1234567890" //defining char set
	var b int
	for b = range PeerIDB {
		PeerIDB[b] = charset[rand.Intn(62)]
	}

	PeerID := string(PeerIDB)

	return PeerID, nil
}

func GetPeers(peersBin []byte) ([]Peer, error) {
	const peerSize = 6 // 4 for IP, 2 for port
	numPeers := len(peersBin) / peerSize
	if len(peersBin)%peerSize != 0 {
		err := fmt.Errorf("received malformed peers")
		return nil, err
	}
	peers := make([]Peer, numPeers)
	for i := 0; i < numPeers; i++ {
		offset := i * peerSize
		peers[i].IP = net.IP(peersBin[offset : offset+4])
		peers[i].Port = binary.BigEndian.Uint16(peersBin[offset+4 : offset+6])
	}
	return peers, nil
}

type Downloaded struct {
	Mu    sync.Mutex
	Piece map[int]([]byte)
	Successful int
	WorkedOn []int
}