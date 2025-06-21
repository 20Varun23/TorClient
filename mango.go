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
				downloaded.WorkedOn[idx] = 1
				return idx
			}
		}
	}
	return -1
}

func HandlePeer(data *utils.BitTorrent, peer int, PeerID string, Pstr []byte, sha1 [20]byte, peerList []utils.Peer, downloaded *utils.Downloaded, totalPieces int, wg *sync.WaitGroup) {

	// at the end of this function, just assume that this function is done
	defer wg.Done()

	//get length of each piece
	pieceLength := data.Info.Piece_length

	// number of blocks that are present in each piece (2^14)
	totalBlocks := int(math.Ceil(float64(pieceLength) / (1 << 14)))

	// handshake object
	c, _ := utils.HandShake(PeerID, Pstr, sha1)

	//object for each peer
	peerObj := utils.Peer{}

	peerObj.IP = peerList[peer].IP
	peerObj.Port = peerList[peer].Port

	str := net.JoinHostPort(peerObj.IP.String(), strconv.Itoa(int(peerObj.Port)))

	//debugging
	//fmt.Printf("\nAddress :%s\n", str)

	//connection lasting for 15 seconds
	conn, err := net.DialTimeout("tcp", str, 15*time.Second)

	if err != nil {
		return
	}

	//close the connection before going out of function
	defer conn.Close()

	// send information through handshake
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

	p := int(pstrP[0]) //protocol identifier length
	restR := make([]byte, p+48)

	_, err = io.ReadFull(conn, restR)

	if err != nil {
		return
	}

	if string(restR[:p])!="BitTorrent protocol" {//Correct protocol
		return
	}

	if string(restR[p+8:p+28])!=string(sha1[:]){ //info hash shld match
		return
	}

	if len(restR) < p+48 {//underfilled
		return
	}

	 
	//debugging
	//fmt.Println("Pstr : ", string(restR[:p]))
	//fmt.Println("Reserved : ", restR[p:p+8])
	//fmt.Printf("Info Hash : %s", string(restR[p+8:p+28]))
	//fmt.Println("Peer ID : ", string(restR[p+28:]))

	// Sending messages
	// bitfield is sent just after handshake
	bitField, err := utils.ReadMsg(conn)

	if err != nil {
		fmt.Println("Could not read bitField", err)
		return
	}

	//fmt.Printf("Length : %d\n", bitField.Length)
	//fmt.Printf("ID : %d\n", bitField.ID)
	//fmt.Printf("Payload : %v", bitField.Payload)

	// totalPieces = len(BitField.Payload)

	//fmt.Println("Debug pre")
	idx := checkPresent(bitField.Payload, downloaded)
	//fmt.Println("Debug post")

	if idx == -1 {
		fmt.Println("\n**************Idx out of -1 *********** ", idx)
		return
	}

	if idx >= len(peerList) {
		fmt.Println("\n************** Idx out of bounds *********** ", idx)
		return
	}


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

	//fmt.Println("\n", interestedMsg)

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


	//keep alive messages
	for message.Length == 0 {
		message, err = utils.ReadMsg(conn)

		if err != nil {
			downloaded.Mu.Lock()
			downloaded.WorkedOn[idx] = 0
			downloaded.Mu.Unlock()
			return
		}

	}


	// send request
	block := 0
	count := 0

	//if last index given then adjust accordingly
	if idx == totalPieces-1 {
		pieceLength = data.Info.Length % (pieceLength * (idx))
	}

	pieceBuffer := make([]byte, pieceLength)


	for block < pieceLength {
		fmt.Println(block, " ", idx)
		//request msg = (1byte of length def) (id i byte) (12 request msg)
		reqPayload := make([]byte, 12)

		//index: integer specifying the zero-based piece index
		//begin: integer specifying the zero-based byte offset within the piece
		//length: integer specifying the requested length.

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
			fmt.Println(err);
			return
		}

		// fmt.Println(reqMsg)

		_, err = conn.Write(reqMsg)
		if err != nil {
			downloaded.Mu.Lock()
			downloaded.WorkedOn[idx] = 0
			downloaded.Mu.Unlock()
			fmt.Println(err);
			return
		}

		message, err = utils.ReadMsg(conn)

		if err != nil {
			downloaded.Mu.Lock()
			downloaded.WorkedOn[idx] = 0
			downloaded.Mu.Unlock()
			fmt.Println(err);
			return
		}



		for message.Length == 0 || message.ID == 1 {
			message, err = utils.ReadMsg(conn)

			if err != nil {
				downloaded.Mu.Lock()
				downloaded.WorkedOn[idx] = 0
				downloaded.Mu.Unlock()
				fmt.Println(err);
				return
			}

			fmt.Println("waiting bro")
		}
		
		if message.ID != 7 {
			downloaded.Mu.Lock()
			downloaded.WorkedOn[idx] = 0
			downloaded.Mu.Unlock()
			fmt.Println(err);
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

	// compare the piece lenghts
	if len(pieceBuffer) != pieceLength {
		fmt.Println("Incorrect piece length")
		downloaded.Mu.Lock()
		downloaded.WorkedOn[idx] = 0
		downloaded.Mu.Unlock()
		return
	}

	//take SHA1 of the piece
	Psha1 := utils.SHA1Bytes(pieceBuffer)

	//take the expected piece hash
	expectedPieceHash := []byte(data.Info.Pieces[idx*20 : (idx+1)*20])


	//Checking if the SHA1 hash is the same or not or is already present
	if bytes.Equal(Psha1[:], expectedPieceHash[:]) {
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
	// file open

	file, err := os.Open(tFile)
	if err != nil {
		panic(err)
	}

	// get torrent object
	data := utils.BitTorrent{}
	err = bencode.Unmarshal(file, &data)

	if err != nil {
		panic(err)
	}

	//file closed
	file.Close()

	sha1, err := utils.SHA1(&data)

	if err != nil {
		panic(err)
	}

	//line to debug
	//fmt.Printf("SHA1 : %x\n", sha1)

	// Creating PeerID
	PeerID, _ := utils.PeerId()

	port := "8080"

	// upload and downloaded number
	downloaded := 0
	uploaded := 0

	//	creating announce url with correct parameters
	aURL, err := utils.AnnounceURL(data.Announce, sha1, PeerID, port, data.Info.Length, downloaded, uploaded)

	if err != nil {
		panic(err)
	}

	//get response
	res, err := http.Get(aURL)
	if err != nil {
		panic(err)
	}

	//read response body
	body, err := io.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}

	aRespObj := utils.Resp{}
	
	// parse the body into response object
	err = bencode.Unmarshal(bytes.NewReader(body), &aRespObj)
	if err != nil {
		panic(err)
	}

	// for debugging 
	//fmt.Printf("Tracker Response: %+v\n", aRespObj) 
	//fmt.Println("Interval : ", aRespObj.Interval)
	//fmt.Println("Peers : ", aRespObj.Peers)

	//* handshake

	var Pstr []byte // Protocol identifier
	Pstr, _ = utils.MakePstr()

	fmt.Println(string(Pstr))

	//getting peerlist
	peerList, _ := utils.GetPeers([]byte(aRespObj.Peers))

	//Number of peers
	totalPeers := len(peerList)

	//debugging
	//fmt.Println("piece length : ", data.Info.Piece_length)
	//fmt.Println("File Length : ", data.Info.Length)

	//caculating the total number of pieces
	totalPieces := int(math.Ceil(float64(data.Info.Length) / float64(data.Info.Piece_length)))

	var downloadedPieces = utils.Downloaded{Piece: make(map[int]([]byte)), WorkedOn: make([]int, totalPieces)}
	fmt.Println(totalPeers)


	for {
		downloadedPieces.Mu.Lock()
		if downloadedPieces.Successful == totalPieces {
			downloadedPieces.Mu.Unlock()
			break
		}

		wg := new(sync.WaitGroup)

		downloadedPieces.Mu.Unlock()
		for peer := 0; peer < totalPeers; peer += 1 {
			// adding one to the wait grp
			wg.Add(1)
			// start goroutine
			go HandlePeer(&data, peer, PeerID, Pstr, sha1, peerList, &downloadedPieces, totalPieces, wg)
		}

		wg.Wait()
	}

	fmt.Println("------------------------------------------------------------------------------------------------")
	fmt.Println("------------------------------------------------------------------------------------------------")

	//make file to add content
	file, err = os.Create(sUrl)
	if err != nil {
		log.Fatalf("Failed to create file: %v", err)
	}
	defer file.Close()

	downloadedPieces.Mu.Lock()
	var keys []int
	// get the piece number for each of the pieces to call from map
	for k := range downloadedPieces.Piece {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	//debugging
	//fmt.Println("Statring Download after count = ", count, "Pieces downloaded = ", downloadedPieces.Successful)

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
	myWindow := myApp.NewWindow("Torrentium")

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
