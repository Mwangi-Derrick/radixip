package go
// RadixError represents errors in the radix engine
type RadixError string

func (e RadixError) Error() string {
	return string(e)
}

const (
	ErrInvalidSubnet RadixError = "invalid subnet"
	ErrInvalidIp RadixError = "invalid Ip"
	ErrInvalidPrefix RadixError = "invalid CIDR Prefix"
	ErrEngine RadixError = "serialization Error"
)