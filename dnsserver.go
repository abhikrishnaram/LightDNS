package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"strings"
)

type DNSHeader struct {
	TransactionID  uint16
	Flags          uint16
	NumQuestions   uint16
	NumAnswers     uint16
	NumAuthorities uint16
	NumAdditionals uint16
}

type DNSResourceRecord struct {
	DomainName         string
	Type               uint16
	Class              uint16
	TimeToLive         uint32
	ResourceDataLength uint16
	ResourceData       []byte
}

const (
	TypeA                  uint16 = 1 // a host address
	ClassINET              uint16 = 1 // the Internet
	FlagResponse           uint16 = 1 << 15
	UDPMaxMessageSizeBytes uint   = 512 // RFC1035
)

func dbLookup(queryResourceRecord DNSResourceRecord) ([]DNSResourceRecord, []DNSResourceRecord, []DNSResourceRecord) {
	var answerResourceRecords = make([]DNSResourceRecord, 0)
	var authorityResourceRecords = make([]DNSResourceRecord, 0)
	var additionalResourceRecords = make([]DNSResourceRecord, 0)

	names, err := GetNames()
	if err != nil {
		return answerResourceRecords, authorityResourceRecords, additionalResourceRecords
	}

	if queryResourceRecord.Type != TypeA || queryResourceRecord.Class != ClassINET {
		return answerResourceRecords, authorityResourceRecords, additionalResourceRecords
	}

	for _, name := range names {
		if strings.Contains(queryResourceRecord.DomainName, name.Name) {
			fmt.Println(queryResourceRecord.DomainName, "resolved to", name.Address)
			answerResourceRecords = append(answerResourceRecords, DNSResourceRecord{
				DomainName:         name.Name,
				Type:               TypeA,
				Class:              ClassINET,
				TimeToLive:         31337,
				ResourceData:       name.Address[12:16],
				ResourceDataLength: 4,
			})
		}
	}

	return answerResourceRecords, authorityResourceRecords, additionalResourceRecords
}

func readDomainName(requestBuffer *bytes.Buffer) (string, error) {
	var domainName string

	b, err := requestBuffer.ReadByte()

	for ; b != 0 && err == nil; b, err = requestBuffer.ReadByte() {
		labelLength := int(b)
		labelBytes := requestBuffer.Next(labelLength)
		labelName := string(labelBytes)

		if len(domainName) == 0 {
			domainName = labelName
		} else {
			domainName += "." + labelName
		}
	}

	return domainName, err
}

func writeDomainName(responseBuffer *bytes.Buffer, domainName string) error {
	labels := strings.Split(domainName, ".")

	for _, label := range labels {
		labelLength := len(label)
		labelBytes := []byte(label)

		responseBuffer.WriteByte(byte(labelLength))
		responseBuffer.Write(labelBytes)
	}

	err := responseBuffer.WriteByte(byte(0))

	return err
}

func handleDNSClient(requestBytes []byte, serverConn *net.UDPConn, clientAddr *net.UDPAddr) {
	var requestBuffer = bytes.NewBuffer(requestBytes)
	var queryHeader DNSHeader
	var queryResourceRecords []DNSResourceRecord

	err := binary.Read(requestBuffer, binary.BigEndian, &queryHeader) // network byte order is big endian

	if err != nil {
		fmt.Println("Error decoding header: ", err.Error())
	}

	queryResourceRecords = make([]DNSResourceRecord, queryHeader.NumQuestions)

	for idx, _ := range queryResourceRecords {
		queryResourceRecords[idx].DomainName, err = readDomainName(requestBuffer)

		if err != nil {
			fmt.Println("Error decoding label: ", err.Error())
		}

		queryResourceRecords[idx].Type = binary.BigEndian.Uint16(requestBuffer.Next(2))
		queryResourceRecords[idx].Class = binary.BigEndian.Uint16(requestBuffer.Next(2))
	}

	var answerResourceRecords = make([]DNSResourceRecord, 0)
	var authorityResourceRecords = make([]DNSResourceRecord, 0)
	var additionalResourceRecords = make([]DNSResourceRecord, 0)

	for _, queryResourceRecord := range queryResourceRecords {
		newAnswerRR, newAuthorityRR, newAdditionalRR := dbLookup(queryResourceRecord)

		answerResourceRecords = append(answerResourceRecords, newAnswerRR...)
		authorityResourceRecords = append(authorityResourceRecords, newAuthorityRR...)
		additionalResourceRecords = append(additionalResourceRecords, newAdditionalRR...)
	}

	var responseBuffer = new(bytes.Buffer)
	var responseHeader DNSHeader

	responseHeader = DNSHeader{
		TransactionID:  queryHeader.TransactionID,
		Flags:          FlagResponse,
		NumQuestions:   queryHeader.NumQuestions,
		NumAnswers:     uint16(len(answerResourceRecords)),
		NumAuthorities: uint16(len(authorityResourceRecords)),
		NumAdditionals: uint16(len(additionalResourceRecords)),
	}

	err = Write(responseBuffer, &responseHeader)

	if err != nil {
		fmt.Println("Error writing to buffer: ", err.Error())
	}

	for _, queryResourceRecord := range queryResourceRecords {
		err = writeDomainName(responseBuffer, queryResourceRecord.DomainName)

		if err != nil {
			fmt.Println("Error writing to buffer: ", err.Error())
		}

		Write(responseBuffer, queryResourceRecord.Type)
		Write(responseBuffer, queryResourceRecord.Class)
	}

	for _, answerResourceRecord := range answerResourceRecords {
		err = writeDomainName(responseBuffer, answerResourceRecord.DomainName)

		if err != nil {
			fmt.Println("Error writing to buffer: ", err.Error())
		}

		Write(responseBuffer, answerResourceRecord.Type)
		Write(responseBuffer, answerResourceRecord.Class)
		Write(responseBuffer, answerResourceRecord.TimeToLive)
		Write(responseBuffer, answerResourceRecord.ResourceDataLength)
		Write(responseBuffer, answerResourceRecord.ResourceData)
	}

	for _, authorityResourceRecord := range authorityResourceRecords {
		err = writeDomainName(responseBuffer, authorityResourceRecord.DomainName)

		if err != nil {
			fmt.Println("Error writing to buffer: ", err.Error())
		}

		Write(responseBuffer, authorityResourceRecord.Type)
		Write(responseBuffer, authorityResourceRecord.Class)
		Write(responseBuffer, authorityResourceRecord.TimeToLive)
		Write(responseBuffer, authorityResourceRecord.ResourceDataLength)
		Write(responseBuffer, authorityResourceRecord.ResourceData)
	}

	for _, additionalResourceRecord := range additionalResourceRecords {
		err = writeDomainName(responseBuffer, additionalResourceRecord.DomainName)

		if err != nil {
			fmt.Println("Error writing to buffer: ", err.Error())
		}

		Write(responseBuffer, additionalResourceRecord.Type)
		Write(responseBuffer, additionalResourceRecord.Class)
		Write(responseBuffer, additionalResourceRecord.TimeToLive)
		Write(responseBuffer, additionalResourceRecord.ResourceDataLength)
		Write(responseBuffer, additionalResourceRecord.ResourceData)
	}

	serverConn.WriteToUDP(responseBuffer.Bytes(), clientAddr)
}

func main() {
	// Initialize in-memory database with hardcoded A records or load from file
	err := LoadFromFile()
	if err != nil {
		fmt.Println("Error loading from file:", err)
	}

	// DNS server setup
	serverAddr, err := net.ResolveUDPAddr("udp", ":1053")
	if err != nil {
		fmt.Println("Error resolving UDP address for DNS server:", err)
		return
	}

	serverConn, err := net.ListenUDP("udp", serverAddr)
	if err != nil {
		fmt.Println("Error listening for DNS server:", err)
		return
	}

	fmt.Println("DNS server is running on :1053")

	// HTTP server setup
	http.HandleFunc("/add-entry", handleAddEntry)

	go func() {
		fmt.Println("HTTP server is running on :8080")
		err := http.ListenAndServe(":8080", nil)
		if err != nil {
			fmt.Println("Error starting HTTP server:", err)
		}
	}()

	defer serverConn.Close()

	// DNS server main loop
	for {
		requestBytes := make([]byte, UDPMaxMessageSizeBytes)

		_, clientAddr, err := serverConn.ReadFromUDP(requestBytes)

		if err != nil {
			fmt.Println("Error receiving for DNS server:", err)
		} else {
			fmt.Println("Received DNS request from ", clientAddr)
			go handleDNSClient(requestBytes, serverConn, clientAddr)
		}
	}
}
