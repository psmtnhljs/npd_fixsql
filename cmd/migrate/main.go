package main

import "log"

func main() {
	log.Println("The legacy SQLite-to-PostgreSQL migration tool has been removed.")
	log.Println("This repository now targets PostgreSQL-only runtime deployments.")
	log.Println("If you need historical SQLite data import, implement it in a separate one-off migration utility.")
}
