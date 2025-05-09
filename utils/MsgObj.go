package utils

import (
	"encoding/binary"
	"fmt"
	"io"
)

type Resp struct {
	Interval int    `bencode:"interval"`
	Peers    string `bencode:"peers"`
}

type TrackerResp struct {
	Interval   int    `bencode:"interval"`
	Tracker_id string `bencode:"tracker id"`
	Complete   int    `bencode:"complete"`
	Incomplete int    `bencode:"incomplete"`
	Peers      string `bencode:"peers"`
}

type Msg struct {
	Length  uint32
	ID      uint8
	Payload []byte
}

func HandShake(peerID string, Pstr []byte, sha1 [20]byte) ([]byte, error) {
	buf := make([]byte, len(Pstr)+49)
	buf[0] = byte(len(Pstr))
	ptr := 1
	ptr += copy(buf[ptr:], Pstr)
	reserved := make([]byte, 8)
	ptr += copy(buf[ptr:], reserved)
	ptr += copy(buf[ptr:], sha1[:])
	ptr += copy(buf[ptr:], []byte(peerID))
	// fmt.Println(buf[0])
	return buf, nil
}

func (i *Msg) MakeMessage() ([]byte, error) {
	buf := make([]byte, i.Length+4)
	binary.BigEndian.PutUint32(buf[0:4], i.Length)

	buf[4] = byte(i.ID)

	_ = copy(buf[5:], i.Payload)

	return buf, nil

}

func ReadMsg(r io.Reader) (*Msg, error) {
	msg := Msg{}

	LengthBytes := make([]byte, 4)
	_, err := io.ReadFull(r, LengthBytes)

	if err != nil {
		fmt.Println("Read length went wrong",err)
		return nil, err
	}

	msg.Length = binary.BigEndian.Uint32(LengthBytes)

	if msg.Length == 0 {
		return &msg, nil
	}

	ID := make([]byte, 1)

	_, err = io.ReadFull(r, ID)

	if err != nil {
		fmt.Println("Read ID went wrong");
		return nil, err
	}

	msg.ID = uint8(ID[0])

	if(msg.Length>1){
	msg.Payload = make([]byte, msg.Length-1)

	_, err = io.ReadFull(r, msg.Payload)
	if err != nil {
		fmt.Println("Read Payload went wrong");
		return nil, err
	}

	}
	return &msg, nil
}