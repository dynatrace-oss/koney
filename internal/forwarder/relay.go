// Copyright (c) 2025 Dynatrace LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package forwarder

import (
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"sync"
	"time"
)

// relayBufferSize is the largest alert that the relay receives in one piece.
const relayBufferSize = 256 * 1024

// relayWriteTimeout bounds a write to a relay that is alive but does not read its socket.
const relayWriteTimeout = time.Second

// RelayAlerts receives alerts as datagrams on a unix socket at path and writes each one as
// a line to out. It runs in the alerts container, so that the log of that container holds
// nothing but alerts. It only returns on an error; the kubelet then restarts the container.
func RelayAlerts(path string, out io.Writer) error {
	// a socket left behind by a previous run of the container would fail the bind
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	buffer := make([]byte, relayBufferSize+1)
	for {
		n, err := conn.Read(buffer[:relayBufferSize])
		if err != nil {
			return err
		}
		buffer[n] = '\n'
		if _, err := out.Write(buffer[:n+1]); err != nil {
			return err
		}
	}
}

// alertRelay sends alerts to the socket of RelayAlerts. Datagrams keep the boundaries of the
// alerts and fail right away while nobody listens, so the forwarder never waits long for the relay.
//
// ponytail: alerts are dropped from the alerts log while the relay is down (they are still
// logged as errors and sent to the sinks); replay them from a small ring buffer if that matters.
type alertRelay struct {
	path  string
	mutex sync.Mutex
	conn  net.Conn
}

// send writes one alert to the relay, dialing it first if there is no connection yet.
func (r *alertRelay) send(alert []byte) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	hadConn := r.conn != nil
	err := r.write(alert)
	// a connection to a relay that has restarted since fails once, so dial the new relay right away
	if err != nil && hadConn {
		err = r.write(alert)
	}

	return err
}

// write dials the relay if needed and writes one alert. The connection is dropped after an error.
func (r *alertRelay) write(alert []byte) error {
	if r.conn == nil {
		conn, err := net.Dial("unixgram", r.path)
		if err != nil {
			return err
		}
		r.conn = conn
	}

	_ = r.conn.SetWriteDeadline(time.Now().Add(relayWriteTimeout))
	if _, err := r.conn.Write(alert); err != nil {
		_ = r.conn.Close()
		r.conn = nil
		return err
	}

	return nil
}
