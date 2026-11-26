package main

// Money is nested in Order.
type Money struct {
	Amount   int    `json:"amount"`
	Currency string `json:"currency"`
	Cents    int    `json:"cents"`
}

type Order struct {
	ID    string `json:"id"`
	Total Money  `json:"total"`
}

// Status is the health answer (new in rev2).
type Status struct {
	OK bool `json:"ok"`
}

type NewOrder struct {
	Items []string `json:"items" binding:"required"`
}
