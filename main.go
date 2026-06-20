// Command pgproof backs up Postgres/MySQL databases, encrypts them, ships them
// to S3/R2/local/Telegram, and proves each backup actually restores.
package main

import (
	"os"

	"github.com/shaxzodbek-uzb/pgproof/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
