package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	utils "bitTorrrent/utils"

	// "fyne.io/fyne/widget"
	"fyne.io/fyne"
	"fyne.io/fyne/app"
	"fyne.io/fyne/container"
	"fyne.io/fyne/dialog"
	"fyne.io/fyne/widget"
	"github.com/jackpal/bencode-go"
)

func checkPresent(BitField []byte, downloaded *utils.Downloaded) int {
	downloaded.Mu.Lock()
	defer downloaded.Mu.Unlock() 

	for i := 0; i < len(BitField); i++ {
		temp := int(BitField[i])
		for j, bitMask := 0, 1<<7; j < 8; j, bitMask = j+1, bitMask>>1 {
			idx := (i * 8) + j
			if (bitMask&temp != 0) && downloaded.WorkedOn[idx] == 0 {
				fmt.Println("In here bro", idx)
				downloaded.WorkedOn[idx] = 1
				return idx
			}
		}
	}
	return -1
}

func HandlePeer(data *utils.BitTorrent, peer int, PeerID string, Pstr []byte, sha1 [20]byte, peerList []utils.Peer, downloaded *utils.Downloaded, totalPieces int, wg *sync.WaitGroup) {

	defer wg.Done()

	pieceLength := data.Info.Piece_length
	fmt.Println(26)

	totalBlocks := int(math.Ceil(float64(pieceLength) / (1 << 14)))

	c, _ := utils.HandShake(PeerID, Pstr, sha1)

	fmt.Printf("%s\n", c)

	peerObj := utils.Peer{}

	peerObj.IP = peerList[peer].IP
	peerObj.Port = peerList[peer].Port

	str := net.JoinHostPort(peerObj.IP.String(), strconv.Itoa(int(peerObj.Port)))

	fmt.Printf("\nAddress :%s\n", str)

	conn, err := net.DialTimeout("tcp", str, 15*time.Second)

	if err != nil {
		return
	}
	defer conn.Close()

	fmt.Println("Hello")

	_, err = conn.Write(c)
	if err != nil {
		fmt.Println("connection problem")
		return
	}

	pstrP := make([]byte, 1)

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, err = io.ReadFull(conn, pstrP)

	if err != nil {
		fmt.Println(err)
		return
	}

	p := int(pstrP[0])
	restR := make([]byte, p+48)

	_, err = io.ReadFull(conn, restR)

	if err != nil {
		fmt.Println("Another reading problem")
		return
	}

	if p == 0 {
		fmt.Println("1")
		return
	}

	if len(restR) < p+48 {
		fmt.Println(2)
		return
	}

	fmt.Println("Pstr : ", string(restR[:p]))
	fmt.Println("Reserved : ", restR[p:p+8])
	fmt.Printf("Info Hash : %s", string(restR[p+8:p+28]))
	fmt.Println("Peer ID : ", string(restR[p+28:]))

	// Sending messages

	bitField, err := utils.ReadMsg(conn)

	if err != nil {
		fmt.Println("Could not read bitField", err)
		return
	}

	fmt.Printf("Length : %d\n", bitField.Length)
	fmt.Printf("ID : %d\n", bitField.ID)
	fmt.Printf("Payload : %v", bitField.Payload)

	// totalPieces = len(BitField.Payload)
	fmt.Println("Debug pre")
	idx := checkPresent(bitField.Payload, downloaded)
	fmt.Println("Debug post")

	// idx:=0;

	if idx == -1 {
		fmt.Println("\n**************Idx out of -1 *********** ", idx)
		return
	}

	if idx >= len(peerList) {
		fmt.Println("\n************** Idx out of bounds *********** ", idx)
		return
	}

	fmt.Println(9090)

	//I am interested packet
	interested := utils.Msg{
		Length: 1,
		ID:     2,
	}

	interestedMsg, err := interested.MakeMessage()

	if err != nil {
		fmt.Println("Could not make interested Message")
		downloaded.Mu.Lock()
		downloaded.WorkedOn[idx] = 0
		downloaded.Mu.Unlock()
		return
	}

	fmt.Println("\n", interestedMsg)

	_, err = conn.Write(interestedMsg)
	if err != nil {
		fmt.Println("Could not write interested Message")
		downloaded.Mu.Lock()
		downloaded.WorkedOn[idx] = 0
		downloaded.Mu.Unlock()
		return
	}

	message, err := utils.ReadMsg(conn)

	if err != nil {
		fmt.Println("Could not Read the the message 1")
		downloaded.Mu.Lock()
		downloaded.WorkedOn[idx] = 0
		downloaded.Mu.Unlock()
		return
	}

	for message.Length == 0 {
		message, err = utils.ReadMsg(conn)

		if err != nil {
			downloaded.Mu.Lock()
			downloaded.WorkedOn[idx] = 0
			downloaded.Mu.Unlock()
			fmt.Println("Oh god")
			return
		}

		fmt.Println("waiting bro")
	}

	fmt.Println(message.ID)

	// send request
	block := 0
	count := 0

	if idx == totalPieces-1 {
		pieceLength = data.Info.Length % (pieceLength * (idx))
	}

	pieceBuffer := make([]byte, pieceLength)
	for block < pieceLength {
		fmt.Println(block, " ", idx)
		reqPayload := make([]byte, 12)

		binary.BigEndian.PutUint32(reqPayload[0:4], uint32(idx))
		binary.BigEndian.PutUint32(reqPayload[4:8], uint32(block))
		if count < totalBlocks-1 {
			binary.BigEndian.PutUint32(reqPayload[8:12], uint32(1<<14))
		} else {

			lastLength := pieceLength - ((totalBlocks - 1) * (1 << 14))
			if lastLength <= 0 {
				fmt.Printf("Invalid last block length: %d\n", lastLength)
				downloaded.Mu.Lock()
				downloaded.WorkedOn[idx] = 0
				downloaded.Mu.Unlock()
				return
			}
			binary.BigEndian.PutUint32(reqPayload[8:12], uint32(lastLength))
		}
		req := utils.Msg{
			Length:  13,
			ID:      6,
			Payload: reqPayload,
		}

		reqMsg, err := req.MakeMessage()

		if err != nil {
			downloaded.Mu.Lock()
			downloaded.WorkedOn[idx] = 0
			downloaded.Mu.Unlock()
			fmt.Println("Ok error here")
			return
		}

		// fmt.Println(reqMsg)

		_, err = conn.Write(reqMsg)
		if err != nil {
			downloaded.Mu.Lock()
			downloaded.WorkedOn[idx] = 0
			downloaded.Mu.Unlock()
			fmt.Println("Ok error is here", err)
			return
		}

		message, err = utils.ReadMsg(conn)

		if err != nil {
			downloaded.Mu.Lock()
			downloaded.WorkedOn[idx] = 0
			downloaded.Mu.Unlock()
			return
		}

		if message.ID != 7 {
			downloaded.Mu.Lock()
			downloaded.WorkedOn[idx] = 0
			downloaded.Mu.Unlock()
			fmt.Println("Unexpected message ID:", message.ID)
			return
		}

		for message.Length == 0 {
			message, err = utils.ReadMsg(conn)

			if err != nil {
				downloaded.Mu.Lock()
				downloaded.WorkedOn[idx] = 0
				downloaded.Mu.Unlock()
				return
			}

			fmt.Println("waiting bro")
		}

		if message.ID == 1 {
			downloaded.Mu.Lock()
			downloaded.WorkedOn[idx] = 0
			downloaded.Mu.Unlock()
			return
		}

		start := block
		var blockLength int
		if count < totalBlocks-1 {
			blockLength = 1 << 14
		} else {
			blockLength = pieceLength - start
		}
		end := start + blockLength
		copy(pieceBuffer[start:end], message.Payload[8:])
		block += 1 << 14
		count++
	}

	fmt.Println("I reached here bro")

	if len(pieceBuffer) != pieceLength {
		fmt.Println("Incorrect piece length")
		downloaded.Mu.Lock()
		downloaded.WorkedOn[idx] = 0
		downloaded.Mu.Unlock()
		return
	}

	Psha1 := utils.SHA1Bytes(pieceBuffer)

	expectedPieceHash := []byte(data.Info.Pieces[idx*20 : (idx+1)*20])

	fmt.Println("REached HerE")

	if bytes.Equal(Psha1[:], expectedPieceHash[:]) {
		fmt.Println("it worked")
		fmt.Println("Worked ", idx)
		downloaded.Mu.Lock()
		if downloaded.WorkedOn[idx] == -1 {
			fmt.Println("Already Present")
			downloaded.Mu.Unlock()
			return
		}
		downloaded.Piece[idx] = pieceBuffer
		downloaded.WorkedOn[idx] = -1
		downloaded.Successful++
		fmt.Println(idx, string(pieceBuffer), "\n\n-----------------------------------------------------------")
		downloaded.Mu.Unlock()
	} else {
		fmt.Println("Not the same piece")
		downloaded.Mu.Lock()
		downloaded.WorkedOn[idx] = -1
		downloaded.Mu.Unlock()
		fmt.Println(Psha1, " ", expectedPieceHash)
	}
}

func torrent(tFile string, sUrl string) {
	file, err := os.Open(tFile)
	if err != nil {
		panic(err)
	}

	data := utils.BitTorrent{}
	err = bencode.Unmarshal(file, &data)

	if err != nil {
		panic(err)
	}

	file.Close()

	sha1, err := utils.SHA1(&data)

	if err != nil {
		panic(err)
	}

	fmt.Printf("SHA1 : %x\n", sha1)

	// Creating PeerID
	PeerID, _ := utils.PeerId()

	port := "8080"

	downloaded := 0
	uploaded := 0

	aURL, err := utils.AnnounceURL(data.Announce, sha1, PeerID, port, data.Info.Length, downloaded, uploaded)

	if err != nil {
		panic(err)
	}

	res, err := http.Get(aURL)
	if err != nil {
		panic(err)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}

	// fmt.Printf("Raw Response: %s\n", string(body))

	aRespObj := utils.Resp{}
	err = bencode.Unmarshal(bytes.NewReader(body), &aRespObj)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Tracker Response: %+v\n", aRespObj)

	fmt.Println("Interval : ", aRespObj.Interval)
	fmt.Println("Peers : ", aRespObj.Peers)

	var Pstr []byte
	Pstr, _ = utils.MakePstr()

	fmt.Println(string(Pstr))

	peerList, _ := utils.GetPeers([]byte(aRespObj.Peers))

	//* Till here dont change
	//* handshake

	//Number of peers
	totalPeers := len(peerList)

	// peer := 0

	fmt.Println("piece length : ", data.Info.Piece_length)
	fmt.Println("File Length : ", data.Info.Length)

	totalPieces := int(math.Ceil(float64(data.Info.Length) / float64(data.Info.Piece_length)))
	var downloadedPieces = utils.Downloaded{Piece: make(map[int]([]byte)), WorkedOn: make([]int, totalPieces)}
	fmt.Println(totalPeers)

	count := 0

	for true {
		count++
		downloadedPieces.Mu.Lock()
		if downloadedPieces.Successful == totalPieces {
			downloadedPieces.Mu.Unlock()
			break
		}

		wg := new(sync.WaitGroup)

		downloadedPieces.Mu.Unlock()
		for peer := 0; peer < totalPeers; peer += 1 {
			wg.Add(1)
			go HandlePeer(&data, peer, PeerID, Pstr, sha1, peerList, &downloadedPieces, totalPieces, wg)
		}

		wg.Wait()
		downloadedPieces.Mu.Lock()
		fmt.Println("=======================", 6767, " ", downloadedPieces.Successful, count, "=================================")
		downloadedPieces.Mu.Unlock()
	}

	fmt.Println("------------------------------------------------------------------------------------------------")
	fmt.Println("------------------------------------------------------------------------------------------------")

	file, err = os.Create(sUrl)
	if err != nil {
		log.Fatalf("Failed to create file: %v", err)
	}
	defer file.Close()

	downloadedPieces.Mu.Lock()
	var keys []int
	for k := range downloadedPieces.Piece {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	fmt.Println(keys)
	fmt.Println()
	fmt.Println()
	fmt.Println()
	fmt.Println("Statring Download after count = ", count, "Pieces downloaded = ", downloadedPieces.Successful)

	for _, k := range keys {
		fmt.Println("Downloading : ", k)
		piece := downloadedPieces.Piece[k]
		if _, err := file.Write(piece); err != nil {
			log.Printf("Error writing piece %d: %v", k, err)
		}
	}
	downloadedPieces.Mu.Unlock()
	file.Close()
}

func main() {

	myApp := app.New()
	myWindow := myApp.NewWindow("File Picker and Saver")

	var tFile string

	var sLoc string

	fileLabel := widget.NewLabel("No file selected")
	saveLabel := widget.NewLabel("No save location selected")

	openFileButton := widget.NewButton("Select File", func() {
		dialog.ShowFileOpen(func(uri fyne.URIReadCloser, err error) {
			if err != nil {
				fileLabel.SetText("Error selecting file")
				return
			}
			if uri == nil {
				fileLabel.SetText("No file selected")
				return
			}

			tFile = uri.URI().String()
			fileLabel.SetText(fmt.Sprintf("Selected: %s", tFile[7:]))
		}, myWindow)
	})

	fileNameEntry := widget.NewEntry()
	fileNameEntry.SetPlaceHolder("Enter file name")

	saveFolderButton := widget.NewButton("Select Save Folder", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				saveLabel.SetText("Error selecting folder")
				return
			}
			if uri == nil {
				saveLabel.SetText("No folder selected")
				return
			}

			sLoc = uri.String()
			saveLabel.SetText(fmt.Sprintf("Save folder: %s/%s", sLoc[7:], fileNameEntry.Text))
		}, myWindow)
	})

	torrentStart := widget.NewButton("Torrent", func() {
		tFile = tFile[7:]
		sLoc = sLoc[7:] + "/" + fileNameEntry.Text
		fmt.Println(sLoc)
		torrent(tFile, sLoc)
	})

	myWindow.SetContent(container.NewVBox(
		openFileButton,
		fileLabel,
		fileNameEntry,
		saveFolderButton,
		saveLabel,
		torrentStart,
	))

	myWindow.ShowAndRun()

}
