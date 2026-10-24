package db

// PartiQL (DynamoDB): double-quoted identifiers, ? parameters, no ILIKE/LIKE. Contains and Has prefix
// are the contains()/begins_with() functions (Funcs), which is also what the DynamoDB driver evaluates.
func init() {
	RegisterDialect(Dialect{Name: "partiql", Quote: quoteWith(`"`), Param: func(int) string { return "?" }, Funcs: true,
		CastText: func(e string) string { return e }})
}
