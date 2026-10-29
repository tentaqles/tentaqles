package drill

import (
	"bytes"
	"time"
)

// DumpFormat is what kind of Postgres dump a file is.
type DumpFormat int

const (
	DumpUnknown DumpFormat = iota
	DumpArchive            // pg_dump -Fc custom archive (pg_restore)
	DumpPlain              // plain SQL (psql -f)
)

// PGDumpHeader reads the start of a Postgres dump: an archive (magic
// PGDMP) and the creation time in its header, or a plain SQL dump. A
// directory-format dump's toc.dat has the same header.
//
// The archive header (pg_backup_archiver.c, WriteHead/ReadHead): "PGDMP",
// version major, minor and revision bytes, int size, offset size, format;
// then the compression (an int before archive 1.15, one byte from 1.15);
// then from 1.4 the creation time as seven ints (sec, min, hour, mday, mon,
// year-1900, isdst). An int is a sign byte plus intSize little-endian bytes.
func PGDumpHeader(head []byte) (DumpFormat, time.Time, bool) {
	if !bytes.HasPrefix(head, []byte("PGDMP")) {
		if plainSQL(head) {
			return DumpPlain, time.Time{}, false
		}
		return DumpUnknown, time.Time{}, false
	}
	b := head[5:]
	if len(b) < 6 {
		return DumpArchive, time.Time{}, false
	}
	vmaj, vmin, vrev, intSize := int(b[0]), int(b[1]), int(b[2]), int(b[3])
	ver := (vmaj*256+vmin)*256 + vrev
	pos := 6 // past offSize and format
	if intSize < 1 || intSize > 8 {
		return DumpArchive, time.Time{}, false
	}
	readInt := func() (int64, bool) {
		if pos+1+intSize > len(b) {
			return 0, false
		}
		sign := b[pos]
		var v int64
		for i := 0; i < intSize; i++ {
			v |= int64(b[pos+1+i]) << (8 * i)
		}
		pos += 1 + intSize
		if sign != 0 {
			v = -v
		}
		return v, true
	}
	v115 := (1*256+15)*256 + 0
	v12 := (1*256+2)*256 + 0
	v14 := (1*256+4)*256 + 0
	switch {
	case ver >= v115:
		pos++ // the compression algorithm byte
	case ver >= v12:
		if _, ok := readInt(); !ok {
			return DumpArchive, time.Time{}, false
		}
	}
	if ver < v14 {
		return DumpArchive, time.Time{}, false
	}
	var tm [7]int64
	for i := range tm {
		v, ok := readInt()
		if !ok {
			return DumpArchive, time.Time{}, false
		}
		tm[i] = v
	}
	sec, min, hour, mday, mon, year := tm[0], tm[1], tm[2], tm[3], tm[4], tm[5]
	if mon < 0 || mon > 11 || mday < 1 || mday > 31 || hour < 0 || hour > 23 || min < 0 || min > 59 || sec < 0 || sec > 61 || year < 70 || year > 300 {
		return DumpArchive, time.Time{}, false
	}
	// pg_dump writes local time (localtime()); read it as local time.
	t := time.Date(int(year)+1900, time.Month(mon+1), int(mday), int(hour), int(min), int(sec), 0, time.Local)
	return DumpArchive, t.UTC(), true
}

func plainSQL(head []byte) bool {
	h := head
	if len(h) > 4096 {
		h = h[:4096]
	}
	h = bytes.TrimPrefix(h, []byte("\xef\xbb\xbf"))
	for _, m := range [][]byte{[]byte("-- PostgreSQL database dump"), []byte("-- PostgreSQL database cluster dump"), []byte("SET statement_timeout"), []byte("CREATE "), []byte("--\n"), []byte("SET ")} {
		if bytes.Contains(h, m) {
			return true
		}
	}
	return false
}
