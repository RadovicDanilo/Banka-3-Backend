package bank

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"github.com/theplant/luhn"
)

var CardSpecs = map[string]struct {
	Prefixes []string
	Length   int
}{
	"Visa":       {Prefixes: []string{"4"}, Length: 16},
	"MasterCard": {Prefixes: []string{"51", "52", "53", "54", "55"}, Length: 16},
	"DinaCard":   {Prefixes: []string{"9891"}, Length: 16},
	"AmEx":       {Prefixes: []string{"34", "37"}, Length: 15},
}

func GenerateCardNumber(cardType string, accountNum string) (string, error) {
	spec, ok := CardSpecs[cardType]
	if !ok {
		return "", fmt.Errorf("invalid card type: %s", cardType)
	}

	prefix := spec.Prefixes[0]
	dataLength := spec.Length - 1

	partialStr := prefix + accountNum
	if len(partialStr) > dataLength {
		partialStr = partialStr[:dataLength]
	} else if len(partialStr) < dataLength {
		partialStr = partialStr + strings.Repeat("0", dataLength-len(partialStr))
	}

	val, err := strconv.ParseInt(partialStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("failed to parse partial card number: %v", err)
	}

	checkDigit := luhn.CalculateLuhn(int(val))

	return fmt.Sprintf("%s%d", partialStr, checkDigit), nil
}

func GenerateCVV() string {
	return fmt.Sprintf("%03d", rand.Intn(1000))
}
