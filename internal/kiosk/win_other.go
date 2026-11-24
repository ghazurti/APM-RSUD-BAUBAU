//go:build !(windows && amd64)

package kiosk

import "time"

const didukung = false

func cariJendela(int, string, time.Duration) (uintptr, error) { return 0, ErrTidakDidukung }
func fokusJendela(uintptr) error                              { return ErrTidakDidukung }
func ketikTeks(string) error                                  { return ErrTidakDidukung }
func tekanTab() error                                         { return ErrTidakDidukung }
func tekanEnter() error                                       { return ErrTidakDidukung }
