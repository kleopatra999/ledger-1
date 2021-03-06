package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/howeyc/ledger/pkg/ledger"
	"github.com/jbrukh/bayesian"
)

func usage() {
	fmt.Printf("Usage: %s -f <ledger-file> <account> <csv file>\n", os.Args[0])
	flag.PrintDefaults()
	os.Exit(1)
}

func main() {
	var ledgerFileName string
	var accountSubstring, csvFileName, csvDateFormat string
	var negateAmount bool

	flag.BoolVar(&negateAmount, "neg", false, "Negate amount column value.")
	flag.StringVar(&ledgerFileName, "f", "", "Ledger file name (*Required).")
	flag.StringVar(&csvDateFormat, "date-format", "01/02/2006", "Date format.")
	flag.Parse()

	args := flag.Args()
	if len(args) != 2 {
		usage()
	} else {
		accountSubstring = args[0]
		csvFileName = args[1]
	}

	csvFileReader, err := os.Open(csvFileName)
	if err != nil {
		fmt.Println("CSV: ", err)
		return
	}
	defer csvFileReader.Close()

	ledgerFileReader, err := os.Open(ledgerFileName)
	if err != nil {
		fmt.Println("Ledger: ", err)
		return
	}
	defer ledgerFileReader.Close()

	generalLedger, parseError := ledger.ParseLedger(ledgerFileReader)
	if parseError != nil {
		fmt.Println(parseError)
		return
	}

	var matchingAccount string
	matchingAccounts := ledger.GetBalances(generalLedger, []string{accountSubstring})
	if len(matchingAccounts) < 1 {
		fmt.Println("Unable to find matching account.")
		return
	} else {
		matchingAccount = matchingAccounts[len(matchingAccounts)-1].Name
	}

	allAccounts := ledger.GetBalances(generalLedger, []string{})

	csvReader := csv.NewReader(csvFileReader)
	csvRecords, _ := csvReader.ReadAll()

	classes := make([]bayesian.Class, len(allAccounts))
	for i, bal := range allAccounts {
		classes[i] = bayesian.Class(bal.Name)
	}
	classifier := bayesian.NewClassifier(classes...)
	for _, tran := range generalLedger {
		payeeWords := strings.Split(tran.Payee, " ")
		for _, accChange := range tran.AccountChanges {
			if strings.Contains(accChange.Name, "Expense") {
				classifier.Learn(payeeWords, bayesian.Class(accChange.Name))
			}
		}
	}

	// Find columns from header
	var dateColumn, payeeColumn, amountColumn int
	dateColumn, payeeColumn, amountColumn = -1, -1, -1
	for fieldIndex, fieldName := range csvRecords[0] {
		fieldName = strings.ToLower(fieldName)
		if strings.Contains(fieldName, "date") {
			dateColumn = fieldIndex
		} else if strings.Contains(fieldName, "description") {
			payeeColumn = fieldIndex
		} else if strings.Contains(fieldName, "amount") {
			amountColumn = fieldIndex
		}
	}

	if dateColumn < 0 || payeeColumn < 0 || amountColumn < 0 {
		fmt.Println("Unable to find columns required from header field names.")
		return
	}

	expenseAccount := ledger.Account{Name: "unknown:unknown", Balance: new(big.Rat)}
	csvAccount := ledger.Account{Name: matchingAccount, Balance: new(big.Rat)}
	for _, record := range csvRecords[1:] {
		inputPayeeWords := strings.Split(record[payeeColumn], " ")
		csvDate, _ := time.Parse(csvDateFormat, record[dateColumn])
		if !existingTransaction(generalLedger, csvDate, inputPayeeWords[0]) {
			// Classify into expense account
			_, likely, _ := classifier.LogScores(inputPayeeWords)
			if likely >= 0 {
				expenseAccount.Name = string(classifier.Classes[likely])
			}

			// Negate amount if required
			expenseAccount.Balance.SetString(record[amountColumn])
			if negateAmount {
				expenseAccount.Balance.Neg(expenseAccount.Balance)
			}

			// Csv amount is the negative of the expense amount
			csvAccount.Balance.Neg(expenseAccount.Balance)

			// Create valid transaction for print in ledger format
			trans := &ledger.Transaction{Date: csvDate, Payee: record[payeeColumn]}
			trans.AccountChanges = []ledger.Account{csvAccount, expenseAccount}
			ledger.PrintTransaction(os.Stdout, trans, 80)
		}
	}
}

func existingTransaction(generalLedger []*ledger.Transaction, transDate time.Time, payee string) bool {
	for _, trans := range generalLedger {
		if trans.Date == transDate && strings.HasPrefix(trans.Payee, payee) {
			return true
		}
	}
	return false
}
