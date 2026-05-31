package cmd

import (
	"fmt"
	"strconv"
)

type eventOption struct {
	enabled bool
	limit   int
}

func (o *eventOption) Set(value string) error {
	switch value {
	case "", "true":
		o.enabled = true
		if o.limit <= 0 {
			o.limit = 5
		}
		return nil
	case "false":
		o.enabled = false
		return nil
	}

	limit, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("events must be true, false, or a positive number")
	}
	if limit < 1 {
		return fmt.Errorf("events limit must be greater than zero")
	}
	o.enabled = true
	o.limit = limit
	return nil
}

func (o eventOption) String() string {
	if !o.enabled {
		return "false"
	}
	return strconv.Itoa(o.limit)
}

func (o eventOption) Type() string {
	return "boolOrInt"
}
