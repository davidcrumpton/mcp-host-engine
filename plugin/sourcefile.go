package plugin

import (
	"fmt"
	"io"
	"os"
)

// maxPluginFileBytes bounds one plugin source file. Everything in plugin_dir is
// read into memory and then handed to the JS runtime, so a FIFO, /dev/zero or a
// stray multi-gigabyte file would otherwise be an unbounded allocation at
// startup or at require() time. 8 MiB is far more than any real plugin.
const maxPluginFileBytes = 8 << 20 // 8 MiB

// readPluginFile reads path with a size cap and a clear error on overrun.
//
// The cap is applied while reading (io.LimitReader) rather than by stat first,
// so a file that grows or is not a regular file at all cannot slip past it.
func readPluginFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxPluginFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPluginFileBytes {
		return nil, fmt.Errorf("%s exceeds the %d byte plugin file limit", path, maxPluginFileBytes)
	}
	return data, nil
}
