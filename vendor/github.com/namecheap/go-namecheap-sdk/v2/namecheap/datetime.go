package namecheap

import "time"

// DateTime represents a time that can be unmarshalled from an XML
type DateTime struct {
	time.Time
}

func (dt DateTime) String() string {
	return dt.Time.String()
}

func (dt *DateTime) UnmarshalText(text []byte) (err error) {
	dt.Time, err = time.Parse("01/02/2006", string(text))
	if err != nil {
		return err
	}

	return nil
}

// Equal reports whether dt and u are equal based on time.Equal
func (dt DateTime) Equal(u DateTime) bool {
	return dt.Time.Equal(u.Time)
}
