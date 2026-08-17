# DoseLedger

DoseLedger is a small command-line ledger for animal-shelter and wildlife-rehabilitation staff. It records one scheduled medication round at a time, keeps every dose outcome, and closes the round into a report that is either `complete` or `attention`.

The application uses only the Go standard library. The JSON ledger is versioned and replaced through a sibling temporary file after writing, syncing, closing, and renaming it into place. A failed update leaves the prior ledger in place.

## Commands

The default ledger path is `doseledger.json`. Pass `--ledger PATH` to use another path. Every successful command writes one JSON object to standard output; failures are written as JSON errors to standard error.

Open a round with one or more scheduled doses. A dose specification uses `id|medication|scheduled-time`.

```text
go run . open --ledger shelter.json --id round-2026-08-17-am --patient "Moss" --dose "amoxicillin|amoxicillin|08:00" --dose "ointment|eye ointment|08:05"
```

Record an administered dose. Supplying `--note` retains the note even when its value is empty; omitting it leaves the note absent.

```text
go run . record --ledger shelter.json --round round-2026-08-17-am --dose amoxicillin --outcome administered --note "with food"
```

Record a skipped dose with a non-blank reason.

```text
go run . record --ledger shelter.json --round round-2026-08-17-am --dose ointment --outcome skipped --reason "patient was resting"
```

Close a fully recorded round once, then inspect its immutable report.

```text
go run . close --ledger shelter.json --round round-2026-08-17-am
go run . show --ledger shelter.json --round round-2026-08-17-am
```

Run the bounded end-to-end smoke workflow in a temporary directory:

```text
go run . smoke
```

## Development

```text
go test ./...
```
