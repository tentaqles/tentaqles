package doctor

func collect(fn func(add func(level, code, ws, msg, fix string))) []Finding {
	var out []Finding
	fn(func(level, code, ws, msg, fix string) {
		out = append(out, Finding{Level: level, Code: code, Workspace: ws, Msg: msg, Fix: fix})
	})
	return out
}
