package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"sync"
)

type NameModel struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

type Name struct {
	Name    string
	Address net.IP
}

func handleAddEntry(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	ip := r.URL.Query().Get("ip")

	if name == "" || ip == "" {
		http.Error(w, "Both 'name' and 'ip' query parameters are required", http.StatusBadRequest)
		return
	}

	// Load existing entries from the file
	existingEntries, err := GetNames()
	if err != nil {
		http.Error(w, fmt.Sprintf("Error loading existing entries: %v", err), http.StatusInternalServerError)
		return
	}

	// Check if the name already exists
	nameExists := false
	for i, entry := range existingEntries {
		if entry.Name == name {
			// Update the existing entry with the new IP
			existingEntries[i].Address = net.ParseIP(ip)
			nameExists = true
			break
		}
	}

	// If the name doesn't exist, add a new entry
	if !nameExists {
		existingEntries = append(existingEntries, Name{
			Name:    name,
			Address: net.ParseIP(ip),
		})
	}

	// Save the updated entries back to the file
	data, err := json.MarshalIndent(existingEntries, "", "    ")
	if err != nil {
		http.Error(w, fmt.Sprintf("Error marshalling data: %v", err), http.StatusInternalServerError)
		return
	}

	err = os.WriteFile("./names.json", data, 0644)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error writing to file: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Added/Updated entry: %s -> %s in the in-memory database", name, ip)
	fmt.Println("Added/Updated entry:", name, "->", ip)
}

type InMemoryDB struct {
	sync.RWMutex
	data map[string]net.IP
}

var nameDB = InMemoryDB{data: make(map[string]net.IP)}

func GetNames() ([]Name, error) {
	// read file
	data, err := os.ReadFile("./names.json")
	if err != nil {
		fmt.Print(err)
		return nil, err
	}
	// json data
	var models []NameModel

	// unmarshall it
	err = json.Unmarshal(data, &models)
	if err != nil {
		fmt.Println("error:", err)
		return nil, err
	}

	return To(models), nil

}

func To(models []NameModel) []Name {
	names := make([]Name, 0, len(models))
	for _, value := range models {
		names = append(names, Name{
			Name:    value.Name,
			Address: net.ParseIP(value.Address),
		})
	}
	return names
}

func LoadFromFile() error {
	data, err := ioutil.ReadFile("./names.json")
	if err != nil {
		// If the file doesn't exist, it's not an error
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("error reading file: %v", err)
	}

	var models []Name
	err = json.Unmarshal(data, &models)
	if err != nil {
		return fmt.Errorf("error unmarshalling data: %v", err)
	}

	nameDB.Lock()
	defer nameDB.Unlock()

	// Clear existing data
	nameDB.data = make(map[string]net.IP)

	// Populate in-memory database
	for _, entry := range models {
		fmt.Println("Adding entry:", entry.Name, "->", entry.Address)
		nameDB.data[entry.Name] = net.ParseIP(string(entry.Address))
	}

	return nil
}
