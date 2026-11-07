// Minimal SMTP sink for the isolated specs. It speaks just enough SMTP for
// Go's net/smtp client: EHLO, STARTTLS (with the self-signed certificate the
// :8083 server trusts through SSL_CERT_FILE), AUTH PLAIN/LOGIN, MAIL, RCPT,
// DATA and QUIT. It accepts any credentials and records each message.
const fs = require('fs');
const net = require('net');
const path = require('path');
const tls = require('tls');

const fixtures = path.join(__dirname, '..', 'fixtures');
const secureContext = tls.createSecureContext({
  key: fs.readFileSync(path.join(fixtures, 'isolated-smtp-key.pem')),
  cert: fs.readFileSync(path.join(fixtures, 'isolated-smtp-cert.pem')),
});

async function startSmtpSink() {
  const messages = [];
  const waiters = [];

  function handle(socket) {
    let secure = false;
    let buffer = '';
    let inData = false;
    let data = [];
    let authLoginStep = 0;

    const reply = (line) => socket.write(line + '\r\n');

    function onLine(line) {
      if (inData) {
        if (line === '.') {
          inData = false;
          const raw = data.join('\n');
          messages.push(raw);
          waiters.splice(0).forEach((resolve) => resolve(raw));
          reply('250 OK queued');
        } else {
          data.push(line.startsWith('..') ? line.slice(1) : line);
        }
        return;
      }
      if (authLoginStep > 0) {
        authLoginStep = authLoginStep === 1 ? 2 : 0;
        reply(authLoginStep === 2 ? '334 UGFzc3dvcmQ6' : '235 Authenticated');
        return;
      }
      const command = line.split(' ')[0].toUpperCase();
      switch (command) {
        case 'EHLO':
        case 'HELO':
          socket.write('250-localhost\r\n');
          if (!secure) socket.write('250-STARTTLS\r\n');
          reply('250 AUTH PLAIN LOGIN');
          break;
        case 'STARTTLS': {
          reply('220 Ready to start TLS');
          socket.removeAllListeners('data');
          const tlsSocket = new tls.TLSSocket(socket, { isServer: true, secureContext });
          secure = true;
          buffer = '';
          attach(tlsSocket);
          socket = tlsSocket;
          break;
        }
        case 'AUTH':
          if (/^AUTH LOGIN/i.test(line)) {
            authLoginStep = 1;
            reply('334 VXNlcm5hbWU6');
          } else {
            reply('235 Authenticated');
          }
          break;
        case 'DATA':
          inData = true;
          data = [];
          reply('354 End data with <CR><LF>.<CR><LF>');
          break;
        case 'QUIT':
          reply('221 Bye');
          socket.end();
          break;
        default:
          reply('250 OK');
      }
    }

    function attach(s) {
      s.on('data', (chunk) => {
        buffer += chunk.toString('utf8');
        let index;
        while ((index = buffer.indexOf('\r\n')) !== -1) {
          const line = buffer.slice(0, index);
          buffer = buffer.slice(index + 2);
          onLine(line);
        }
      });
      s.on('error', () => {});
    }

    attach(socket);
    reply('220 localhost fake SMTP sink');
  }

  const server = net.createServer(handle);
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));

  return {
    host: '127.0.0.1',
    port: server.address().port,
    messages,
    nextMessage() {
      return new Promise((resolve) => waiters.push(resolve));
    },
    close() {
      return new Promise((resolve) => server.close(() => resolve()));
    },
  };
}

module.exports = { startSmtpSink };
