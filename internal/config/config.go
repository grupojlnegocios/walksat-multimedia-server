package config

type Config struct {
	TCPListenAddr string
	StreamRoot    string
	HlsTime       int
	HlsListSize   int
}

func Default() Config {
	return Config{
		TCPListenAddr: ":6207",
		StreamRoot:    "./streams",
		HlsTime:       2,
		HlsListSize:   6,
	}
}
