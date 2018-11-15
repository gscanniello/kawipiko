

package main


import "bytes"
import "flag"
import "fmt"
import "io"
import "log"
import "net"
import "net/http"
import "os"
import "os/signal"
import "runtime"
import "runtime/debug"
import "runtime/pprof"
import "sync"
import "syscall"
import "time"
import "unsafe"

// import "github.com/colinmarc/cdb"
import cdb "github.com/cipriancraciun/go-cdb-lib"

import "github.com/valyala/fasthttp"
import "github.com/valyala/fasthttp/reuseport"

import . "github.com/cipriancraciun/go-cdb-http/lib/common"
import . "github.com/cipriancraciun/go-cdb-http/lib/server"




type server struct {
	httpServer *fasthttp.Server
	cdbReader *cdb.CDB
	debug bool
}




func (_server *server) Serve (_context *fasthttp.RequestCtx) () {
	
	// _request := (*fasthttp.Request) (NoEscape (unsafe.Pointer (&_context.Request)))
	_requestHeaders := (*fasthttp.RequestHeader) (NoEscape (unsafe.Pointer (&_context.Request.Header)))
	_response := (*fasthttp.Response) (NoEscape (unsafe.Pointer (&_context.Response)))
	_responseHeaders := (*fasthttp.ResponseHeader) (NoEscape (unsafe.Pointer (&_context.Response.Header)))
	
	_keyBuffer := [1024]byte {}
	_pathBuffer := [1024]byte {}
	_timestampBuffer := [128]byte {}
	
	_timestamp := time.Now ()
	_timestampHttp := _timestamp.AppendFormat (_timestampBuffer[:0], http.TimeFormat)
	
	_responseHeaders.SetCanonical ([]byte ("Date"), _timestampHttp)
	
	_method := _requestHeaders.Method ()
	
	_path := append (_pathBuffer[:0], _requestHeaders.RequestURI () ...)
	if _pathLimit := bytes.IndexByte (_path, '?'); _pathLimit > 0 {
		_path = _path[: _pathLimit]
	}
	// FIXME:  Decode path according to `decodeArgAppendNoPlus`!
	
	_pathLen := len (_path)
	_pathIsRoot := _pathLen == 1
	_pathHasSlash := !_pathIsRoot && (_path[_pathLen - 1] == '/')
	
	if ! bytes.Equal ([]byte (http.MethodGet), _method) {
		log.Printf ("[ww] [bce7a75b] invalid method `%s` for `%s`!\n", _requestHeaders.Method (), _requestHeaders.RequestURI ())
		_server.ServeError (_context, http.StatusMethodNotAllowed, nil, true)
		return
	}
	if (_pathLen == 0) || (_path[0] != '/') {
		log.Printf ("[ww] [fa6b1923] invalid path `%s`!\n", _requestHeaders.RequestURI ())
		_server.ServeError (_context, http.StatusBadRequest, nil, true)
		return
	}
	
	if bytes.HasPrefix (_path, []byte ("/__/")) {
		if bytes.Equal (_path, []byte ("/__/heartbeat")) || bytes.HasPrefix (_path, []byte ("/__/heartbeat/")) {
			_server.ServeStatic (_context, http.StatusOK, HeartbeatDataOk, HeartbeatContentType, HeartbeatContentEncoding, false)
		} else {
			_server.ServeError (_context, http.StatusNotFound, nil, true)
		}
		return
	}
	
	// _responseHeaders.SetCanonical ([]byte ("Content-Security-Policy"), []byte ("upgrade-insecure-requests"))
	_responseHeaders.SetCanonical ([]byte ("Referrer-Policy"), []byte ("strict-origin-when-cross-origin"))
	_responseHeaders.SetCanonical ([]byte ("X-Frame-Options"), []byte ("SAMEORIGIN"))
	_responseHeaders.SetCanonical ([]byte ("X-content-type-Options"), []byte ("nosniff"))
	_responseHeaders.SetCanonical ([]byte ("X-XSS-Protection"), []byte ("1; mode=block"))
	
	var _fingerprint []byte
	{
		_found : for _, _namespace := range []string {NamespaceFilesContent, NamespaceFoldersContent} {
			_key := _keyBuffer[:0]
			_key = append (_key, _namespace ...)
			_key = append (_key, ':')
			_key = append (_key, _path ...)
			if _value, _error := _server.cdbReader.GetWithCdbHash (_key); _error == nil {
				if _value != nil {
					_fingerprint = _value
					if (_namespace == NamespaceFoldersContent) {
						if !_pathIsRoot && !_pathHasSlash {
							_path = append (_path, '/')
							_server.ServeRedirect (_context, http.StatusTemporaryRedirect, _path, true)
							return
						}
					}
					break _found
				}
			} else {
				_server.ServeError (_context, http.StatusInternalServerError, _error, false)
				return
			}
		}
	}
	
	if _fingerprint == nil {
		if ! bytes.Equal ([]byte ("/favicon.ico"), _path) {
			log.Printf ("[ww] [7416f61d]  not found `%s`!\n", _requestHeaders.RequestURI ())
			_server.ServeError (_context, http.StatusNotFound, nil, true)
		} else {
			_server.ServeStatic (_context, http.StatusOK, FaviconData, FaviconContentType, FaviconContentEncoding, true)
		}
		return
	}
	
	var _data []byte
	{
		_key := _keyBuffer[:0]
		_key = append (_key, NamespaceDataContent ...)
		_key = append (_key, ':')
		_key = append (_key, _fingerprint ...)
		if _value, _error := _server.cdbReader.GetWithCdbHash (_key); _error == nil {
			if _value != nil {
				_data = _value
			} else {
				log.Printf ("[ee] [0165c193]  missing data content for `%s`!\n", _requestHeaders.RequestURI ())
				_server.ServeError (_context, http.StatusInternalServerError, nil, false)
				return
			}
		} else {
			_server.ServeError (_context, http.StatusInternalServerError, _error, false)
			return
		}
	}
	
	{
		_key := _keyBuffer[:0]
		_key = append (_key, NamespaceDataMetadata ...)
		_key = append (_key, ':')
		_key = append (_key, _fingerprint ...)
		if _value, _error := _server.cdbReader.GetWithCdbHash (_key); _error == nil {
			if _value != nil {
				if _error := MetadataDecodeIterate (_value, _responseHeaders.SetCanonical); _error == nil {
				} else {
					_server.ServeError (_context, http.StatusInternalServerError, _error, false)
					return
				}
			} else {
				log.Printf ("[ee] [e8702411]  missing data metadata for `%s`!\n", _requestHeaders.RequestURI ())
				_server.ServeError (_context, http.StatusInternalServerError, nil, false)
				return
			}
		} else {
			_server.ServeError (_context, http.StatusInternalServerError, _error, false)
			return
		}
	}
	
	if _server.debug {
		log.Printf ("[dd] [b15f3cad]  serving for `%s`...\n", _requestHeaders.RequestURI ())
	}
	
	_responseHeaders.SetCanonical ([]byte ("Cache-Control"), []byte ("public, immutable, max-age=3600"))
	
	_response.SetStatusCode (http.StatusOK)
	_response.SetBodyRaw (_data)
}




func (_server *server) ServeStatic (_context *fasthttp.RequestCtx, _status uint, _data []byte, _contentType string, _contentEncoding string, _cache bool) () {
	
	_response := (*fasthttp.Response) (NoEscape (unsafe.Pointer (&_context.Response)))
	_responseHeaders := (*fasthttp.ResponseHeader) (NoEscape (unsafe.Pointer (&_context.Response.Header)))
	
	_responseHeaders.SetCanonical ([]byte ("Content-Type"), []byte (_contentType))
	_responseHeaders.SetCanonical ([]byte ("Content-Encoding"), []byte (_contentEncoding))
	
	if _cache {
		_responseHeaders.SetCanonical ([]byte ("Cache-Control"), []byte ("public, immutable, max-age=3600"))
	} else {
		_responseHeaders.SetCanonical ([]byte ("Cache-Control"), []byte ("no-cache"))
	}
	
	_response.SetStatusCode (int (_status))
	_response.SetBodyRaw (_data)
}


func (_server *server) ServeRedirect (_context *fasthttp.RequestCtx, _status uint, _path []byte, _cache bool) () {
	
	_response := (*fasthttp.Response) (NoEscape (unsafe.Pointer (&_context.Response)))
	_responseHeaders := (*fasthttp.ResponseHeader) (NoEscape (unsafe.Pointer (&_context.Response.Header)))
	
	_responseHeaders.SetCanonical ([]byte ("Content-Encoding"), []byte ("identity"))
	_responseHeaders.SetCanonical ([]byte ("Location"), _path)
	
	if _cache {
		_responseHeaders.SetCanonical ([]byte ("Cache-Control"), []byte ("public, immutable, max-age=3600"))
	} else {
		_responseHeaders.SetCanonical ([]byte ("Cache-Control"), []byte ("no-cache"))
	}
	
	// _responseHeaders.SetCanonical ([]byte ("Content-Type"), []byte (MimeTypeText))
	_response.SetStatusCode (int (_status))
	// _response.SetBodyRaw ([]byte (fmt.Sprintf ("[%d] %s", _status, _path)))
}


func (_server *server) ServeError (_context *fasthttp.RequestCtx, _status uint, _error error, _cache bool) () {
	
	_response := (*fasthttp.Response) (NoEscape (unsafe.Pointer (&_context.Response)))
	_responseHeaders := (*fasthttp.ResponseHeader) (NoEscape (unsafe.Pointer (&_context.Response.Header)))
	
	if _cache {
		_responseHeaders.SetCanonical ([]byte ("Cache-Control"), []byte ("public, immutable, max-age=3600"))
	} else {
		_responseHeaders.SetCanonical ([]byte ("Cache-Control"), []byte ("no-cache"))
	}
	
	// _responseHeaders.SetCanonical ([]byte ("Content-Type"), []byte (MimeTypeText))
	// _responseHeaders.SetCanonical ([]byte ("Content-Encoding"), []byte ("identity"))
	_response.SetStatusCode (int (_status))
	// _response.SetBodyRaw ([]byte (fmt.Sprintf ("[%d]", _status)))
	
	LogError (_error, "")
}




func main () () {
	Main (main_0)
}


func main_0 () (error) {
	
	
	var _bind string
	var _archive string
	var _archiveInmem bool
	var _archiveMmap bool
	var _archivePreload bool
	var _processes uint
	var _threads uint
	var _debug bool
	
	var _profileCpu string
	var _profileMem string
	
	{
		_flags := flag.NewFlagSet ("cdb-http-server", flag.ContinueOnError)
		
		_flags.Usage = func () () {
			fmt.Fprintf (os.Stderr, "%s",
`
cdb-http-server
	--archive <path>
	--archive-inmem      (memory-loaded archive file)
	--archive-mmap       (memory-mapped archive file)
	--archive-preload    (preload archive file)
	--bind <ip>:<port>
	--processes <count>  (of slave processes)
	--threads <count>    (of threads per process)
	--profile-cpu <path>
	--profile-mem <path>
	--debug
`)
		}
		
		_bind_0 := _flags.String ("bind", "", "")
		_archive_0 := _flags.String ("archive", "", "")
		_archiveInmem_0 := _flags.Bool ("archive-inmem", false, "")
		_archiveMmap_0 := _flags.Bool ("archive-mmap", false, "")
		_archivePreload_0 := _flags.Bool ("archive-preload", false, "")
		_processes_0 := _flags.Uint ("processes", 0, "")
		_threads_0 := _flags.Uint ("threads", 0, "")
		_profileCpu_0 := _flags.String ("profile-cpu", "", "")
		_profileMem_0 := _flags.String ("profile-mem", "", "")
		_debug_0 := _flags.Bool ("debug", false, "")
		
		FlagsParse (_flags, 0, 0)
		
		_bind = *_bind_0
		_archive = *_archive_0
		_archiveInmem = *_archiveInmem_0
		_archiveMmap = *_archiveMmap_0
		_archivePreload = *_archivePreload_0
		_processes = *_processes_0
		_threads = *_threads_0
		_debug = *_debug_0
		
		_profileCpu = *_profileCpu_0
		_profileMem = *_profileMem_0
		
		if _bind == "" {
			AbortError (nil, "[6edd9512]  expected bind address argument!")
		}
		if _archive == "" {
			AbortError (nil, "[eefe1a38]  expected archive file argument!")
		}
		
		if _archiveInmem && _archiveMmap {
			AbortError (nil, "[a2101041]  archive 'memory-loaded' and 'memory-mapped' are mutually exclusive!")
		}
		if _archiveInmem && _archivePreload {
			log.Printf ("[ww] [3e8a40e4]  archive 'memory-loaded' implies preloading!\n")
			_archivePreload = false
		}
		
		if (_processes > 1) && ((_profileCpu != "") || (_profileMem != "")) {
			AbortError (nil, "[cd18d250]  multi-process and profiling are mutually exclusive!")
		}
		
		if _processes < 1 {
			_processes = 1
		}
		if _threads < 1 {
			_threads = 1
		}
	}
	
	
	runtime.GOMAXPROCS (int (_threads))
	
	debug.SetGCPercent (50)
	debug.SetMaxThreads (128)
	debug.SetMaxStack (16 * 1024)
	
	_httpServerReduceMemory := false
	
	if false {
		if _error := syscall.Setrlimit (syscall.RLIMIT_DATA, & syscall.Rlimit { Max : 4 * 1024 * 1024 * 1024 }); _error != nil {
			AbortError (_error, "[f661b4fe]  failed to configure limits!")
		}
	}
	
	
	if _processes > 1 {
		
		log.Printf ("[ii] [06f8c944]  sub-processes starting (`%d` processes with `%d` threads each)...\n", _processes, _threads)
		
		_processesJoin := & sync.WaitGroup {}
		
		_processesPid := make ([]*os.Process, _processes)
		
		_processName := os.Args[0]
		_processArguments := make ([]string, 0, len (os.Args))
		_processArguments = append (_processArguments,
				_processName,
				"--bind", _bind,
				"--archive", _archive,
			)
		if _archiveInmem {
			_processArguments = append (_processArguments, "--archive-inmem")
		}
		if _archiveMmap {
			_processArguments = append (_processArguments, "--archive-mmap")
		}
		if _archivePreload {
			_processArguments = append (_processArguments, "--archive-preload")
		}
		if _debug {
			_processArguments = append (_processArguments, "--debug")
		}
		_processArguments = append (_processArguments, "--threads", fmt.Sprintf ("%d", _threads))
		
		_processAttributes := & os.ProcAttr {
				Env : []string {},
				Files : []*os.File {
						os.Stdin,
						os.Stdout,
						os.Stderr,
					},
				Sys : nil,
			}
		
		for _processIndex, _ := range _processesPid {
			if _processPid, _error := os.StartProcess (_processName, _processArguments, _processAttributes); _error == nil {
				_processesJoin.Add (1)
				_processesPid[_processIndex] = _processPid
				log.Printf ("[ii] [63cb22f8]  sub-process `%d` started;\n", _processPid.Pid)
				go func (_index int, _processPid *os.Process) () {
					if _processStatus, _error := _processPid.Wait (); _error == nil {
						if _processStatus.Success () {
							log.Printf ("[ii] [66b60b81]  sub-process `%d` succeeded;\n", _processPid.Pid)
						} else {
							log.Printf ("[ww] [5d25046b]  sub-process `%d` failed:  `%s`;  ignoring!\n", _processPid.Pid, _processStatus)
						}
					} else {
						LogError (_error, fmt.Sprintf ("[f1bfc927]  failed waiting for sub-process `%d`;  ignoring!", _processPid.Pid))
					}
					_processesPid[_processIndex] = nil
					_processesJoin.Done ()
				} (_processIndex, _processPid)
			} else {
				LogError (_error, "[8892b34d]  failed starting sub-process;  ignoring!")
			}
		}
		
		{
			_signals := make (chan os.Signal, 32)
			signal.Notify (_signals, syscall.SIGINT, syscall.SIGTERM)
			go func () () {
				for {
					_signal := <- _signals
					log.Printf ("[ii] [a9243ecb]  signaling sub-processes...\n")
					for _, _processPid := range _processesPid {
						if _processPid != nil {
							if _error := _processPid.Signal (_signal); _error != nil {
								LogError (_error, fmt.Sprintf ("[ab681164]  failed signaling sub-process `%d`;  ignoring!", _processPid.Pid))
							}
						}
					}
				}
			} ()
		}
		
		_processesJoin.Wait ()
		
		log.Printf ("[ii] [b949bafc]  sub-processes terminated;\n")
		
		return nil
	}
	
	
	var _cdbReader *cdb.CDB
	{
		log.Printf ("[ii] [3b788396]  opening archive file `%s`...\n", _archive)
		
		var _cdbFile *os.File
		if _cdbFile_0, _error := os.Open (_archive); _error == nil {
			_cdbFile = _cdbFile_0
		} else {
			AbortError (_error, "[9e0b5ed3]  failed opening archive file!")
		}
		
		var _cdbFileSize int
		{
			var _cdbFileSize_0 int64
			if _cdbFileStat, _error := _cdbFile.Stat (); _error == nil {
				_cdbFileSize_0 = _cdbFileStat.Size ()
			} else {
				AbortError (_error, "[0ccf0a3b]  failed opening archive file!")
			}
			if _cdbFileSize_0 < 1024 {
				AbortError (nil, "[6635a2a8]  failed opening archive:  file is too small (or empty)!")
			}
			if _cdbFileSize_0 >= (2 * 1024 * 1024 * 1024) {
				AbortError (nil, "[545bf6ce]  failed opening archive:  file is too large!")
			}
			_cdbFileSize = int (_cdbFileSize_0)
		}
		
		if _archivePreload {
			log.Printf ("[ii] [13f4ebf7]  preloading archive file...\n")
			_buffer := [16 * 1024]byte {}
			_loop : for {
				switch _, _error := _cdbFile.Read (_buffer[:]); _error {
					case io.EOF :
						break _loop
					case nil :
						continue _loop
					default :
						AbortError (_error, "[a1c3b922]  failed preloading archive file...\n")
				}
			}
		}
		
		if _archiveInmem || _archiveMmap {
			
			var _cdbData []byte
			
			if _archiveInmem {
				
				log.Printf ("[ii] [216e584b]  opening memory-loaded archive...\n")
				
				_cdbData = make ([]byte, _cdbFileSize)
				if _, _error := io.ReadFull (_cdbFile, _cdbData); _error != nil {
					AbortError (_error, "[73039784]  failed loading archive file!")
				}
				
			} else if _archiveMmap {
				
				log.Printf ("[ii] [f47fae8a]  opening memory-mapped archive...\n")
				
				if _cdbData_0, _error := syscall.Mmap (int (_cdbFile.Fd ()), 0, int (_cdbFileSize), syscall.PROT_READ, syscall.MAP_SHARED); _error == nil {
					_cdbData = _cdbData_0
				} else {
					AbortError (_error, "[c0e2632c]  failed mapping archive file!")
				}
				
				if _archivePreload {
					log.Printf ("[ii] [d96b06c9]  preloading memory-loaded archive...\n")
					_buffer := [16 * 1024]byte {}
					_bufferOffset := 0
					for {
						if _bufferOffset == _cdbFileSize {
							break
						}
						_bufferOffset += copy (_buffer[:], _cdbData[_bufferOffset:])
					}
				}
				
			} else {
				panic ("e4fffcd8")
			}
			
			if _error := _cdbFile.Close (); _error != nil {
				AbortError (_error, "[5e0449c2]  failed closing archive file!")
			}
			
			if _cdbReader_0, _error := cdb.NewFromBufferWithHasher (_cdbData, nil); _error == nil {
				_cdbReader = _cdbReader_0
			} else {
				AbortError (_error, "[27e4813e]  failed opening archive!")
			}
			
		} else {
			
			log.Printf ("[ww] [dd697a66]  using `read`-based archive (with significant performance impact)!\n")
			
			if _cdbReader_0, _error := cdb.NewFromReaderWithHasher (_cdbFile, nil); _error == nil {
				_cdbReader = _cdbReader_0
			} else {
				AbortError (_error, "[35832022]  failed opening archive!")
			}
			
		}
	}
	
	
	_server := & server {
			httpServer : nil,
			cdbReader : _cdbReader,
			debug : _debug,
		}
	
	
	if _profileCpu != "" {
		log.Printf ("[ii] [70c210f3]  profiling CPU to `%s`...\n", _profileCpu)
		_stream, _error := os.Create (_profileCpu)
		if _error != nil {
			AbortError (_error, "[fd4e0009]  failed opening CPU profile!")
		}
		_error = pprof.StartCPUProfile (_stream)
		if _error != nil {
			AbortError (_error, "[ac721629]  failed starting CPU profile!")
		}
		defer pprof.StopCPUProfile ()
	}
	if _profileMem != "" {
		log.Printf ("[ii] [9196ee90]  profiling MEM to `%s`...\n", _profileMem)
		_stream, _error := os.Create (_profileMem)
		if _error != nil {
			AbortError (_error, "[907d08b5]  failed opening MEM profile!")
		}
		_profile := pprof.Lookup ("heap")
		defer func () () {
			runtime.GC ()
			if _profile != nil {
				if _error := _profile.WriteTo (_stream, 0); _error != nil {
					AbortError (_error, "[4b1e5112]  failed writing MEM profile!")
				}
			} else {
				AbortError (nil, "[385dc8f0]  failed loading MEM profile!")
			}
			_stream.Close ()
		} ()
	}
	
	
	_httpServer := & fasthttp.Server {
			
			Name : "cdb-http",
			Handler : _server.Serve,
			
			NoDefaultServerHeader : true,
			NoDefaultContentType : true,
			DisableHeaderNamesNormalizing : true,
			
			Concurrency : 4 * 1024,
			
			ReadBufferSize : 2 * 1024,
			WriteBufferSize : 2 * 1024,
			
			ReadTimeout : 6 * time.Second,
			WriteTimeout : 6 * time.Second,
			MaxKeepaliveDuration : 360 * time.Second,
			MaxRequestsPerConn : 256 * 1024,
			MaxRequestBodySize : 2 * 1024,
			GetOnly : true,
			
			TCPKeepalive : true,
			TCPKeepalivePeriod : 6 * time.Second,
			
			ReduceMemoryUsage : _httpServerReduceMemory,
			
		}
	
	_server.httpServer = _httpServer
	
	
	{
		_signals := make (chan os.Signal, 32)
		signal.Notify (_signals, syscall.SIGINT, syscall.SIGTERM)
		go func () () {
			<- _signals
			log.Printf ("[ii] [691cb695]  shutingdown...\n")
			_server.httpServer.Shutdown ()
		} ()
	}
	
	
	log.Printf ("[ii] [f11e4e37]  listening on `http://%s/`;\n", _bind)
	
	var _httpListener net.Listener
	if _httpListener_0, _error := reuseport.Listen ("tcp4", _bind); _error == nil {
		_httpListener = _httpListener_0
	} else {
		AbortError (_error, "[d5f51e9f]  failed starting listener!")
	}
	
	if _error := _httpServer.Serve (_httpListener); _error != nil {
		AbortError (_error, "[44f45c67]  failed executing server!")
	}
	
	
	defer log.Printf ("[ii] [a49175db]  done!\n")
	return nil
}

