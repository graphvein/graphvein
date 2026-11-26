package main

// Money is nested in Order.
type Money struct {
	Amount   int    `json:"amount"`
	Currency string `json:"currency"`
}

type Order struct {
	ID    string `json:"id"`
	Total Money  `json:"total"`
}

type NewOrder struct {
	Items []string `json:"items" binding:"required"`
}
