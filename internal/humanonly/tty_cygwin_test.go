package humanonly

import "testing"

func TestCygwinPtyName(t *testing.T) {
	cases := map[string]bool{
		`\msys-dd50a72ab4668b33-pty0-from-master`:                  true,
		`\msys-dd50a72ab4668b33-pty0-to-master`:                    true,
		`\msys-dd50a72ab4668b33-pty12-from-master`:                 true,
		`\cygwin-e022582115c10879-pty3-to-master`:                  true,
		`\Device\NamedPipe\msys-dd50a72ab4668b33-pty1-from-master`: true,
		`\Device\NamedPipe\cygwin-e022582115c10879-pty0-to-master`: true,
		`\MSYS-dd50a72ab4668b33-pty0-from-master`:                  true,
		// not a pty: anonymous and other named pipes, a console, a file
		`\msys-dd50a72ab4668b33-pty0-from-slave`:                 false,
		`\msys-dd50a72ab4668b33-pty0-master`:                     false,
		`\msys--pty0-from-master`:                                false,
		`\msys-dd50a72ab4668b33-tty0-from-master`:                false,
		`\mysys-dd50a72ab4668b33-pty0-from-master`:               false,
		`\Device\NamedPipe\Win32Pipes.00001234.0000000000000001`: false,
		`\pipe\docker_engine`:                                    false,
		`msys-dd50a72ab4668b33-pty0-from-master`:                 false,
		``:                                                       false,
		`-`:                                                      false,
	}
	for name, want := range cases {
		if got := cygwinPtyName(name); got != want {
			t.Errorf("cygwinPtyName(%q) = %v, want %v", name, got, want)
		}
	}
}
