"use strict";
var __create = Object.create;
var __defProp = Object.defineProperty;
var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __getProtoOf = Object.getPrototypeOf;
var __hasOwnProp = Object.prototype.hasOwnProperty;
var __export = (target, all) => {
  for (var name in all)
    __defProp(target, name, { get: all[name], enumerable: true });
};
var __copyProps = (to, from, except, desc) => {
  if (from && typeof from === "object" || typeof from === "function") {
    for (let key of __getOwnPropNames(from))
      if (!__hasOwnProp.call(to, key) && key !== except)
        __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
  }
  return to;
};
var __toESM = (mod, isNodeMode, target) => (target = mod != null ? __create(__getProtoOf(mod)) : {}, __copyProps(
  // If the importer is in node compatibility mode or this is not an ESM
  // file that has been converted to a CommonJS file using a Babel-
  // compatible transform (i.e. "__esModule" has not been set), then set
  // "default" to the CommonJS "module.exports" for node compatibility.
  isNodeMode || !mod || !mod.__esModule ? __defProp(target, "default", { value: mod, enumerable: true }) : target,
  mod
));
var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);
var socksClientCertificatesInterceptor_exports = {};
__export(socksClientCertificatesInterceptor_exports, {
  ClientCertificatesProxy: () => ClientCertificatesProxy,
  getMatchingTLSOptionsForOrigin: () => getMatchingTLSOptionsForOrigin,
  rewriteOpenSSLErrorIfNeeded: () => rewriteOpenSSLErrorIfNeeded
});
module.exports = __toCommonJS(socksClientCertificatesInterceptor_exports);
var import_events = require("events");
var import_http2 = __toESM(require("http2"));
var import_net = __toESM(require("net"));
var import_stream = __toESM(require("stream"));
var import_tls = __toESM(require("tls"));
var import_socksProxy = require("./utils/socksProxy");
var import_utils = require("../utils");
var import_browserContext = require("./browserContext");
var import_network = require("./utils/network");
var import_debugLogger = require("./utils/debugLogger");
var import_happyEyeballs = require("./utils/happyEyeballs");
let dummyServerTlsOptions = void 0;
function loadDummyServerCertsIfNeeded() {
  if (dummyServerTlsOptions)
    return;
  const { cert, key } = (0, import_utils.generateSelfSignedCertificate)();
  dummyServerTlsOptions = { key, cert };
}
class ALPNCache {
  constructor() {
    this._cache = /* @__PURE__ */ new Map();
  }
  get(host, port, success) {
    const cacheKey = `${host}:${port}`;
    {
      const result2 = this._cache.get(cacheKey);
      if (result2) {
        result2.then(success);
        return;
      }
    }
    const result = new import_utils.ManualPromise();
    this._cache.set(cacheKey, result);
    result.then(success);
    (0, import_happyEyeballs.createTLSSocket)({
      host,
      port,
      servername: import_net.default.isIP(host) ? void 0 : host,
      ALPNProtocols: ["h2", "http/1.1"],
      rejectUnauthorized: false
    }).then((socket) => {
      result.resolve(socket.alpnProtocol || "http/1.1");
      socket.end();
    }).catch((error) => {
      import_debugLogger.debugLogger.log("client-certificates", `ALPN error: ${error.message}`);
      result.resolve("http/1.1");
    });
  }
}
class SocksProxyConnection {
  constructor(socksProxy, uid, host, port) {
    this.firstPackageReceived = false;
    this._closed = false;
    this.socksProxy = socksProxy;
    this.uid = uid;
    this.host = host;
    this.port = port;
    this._targetCloseEventListener = () => {
      this.socksProxy._socksProxy.sendSocketEnd({ uid: this.uid });
      this.internalTLS?.destroy();
      this._dummyServer?.close();
    };
  }
  async connect() {
    if (this.socksProxy.proxyAgentFromOptions)
      this.target = await this.socksProxy.proxyAgentFromOptions.callback(new import_events.EventEmitter(), { host: rewriteToLocalhostIfNeeded(this.host), port: this.port, secureEndpoint: false });
    else
      this.target = await (0, import_happyEyeballs.createSocket)(rewriteToLocalhostIfNeeded(this.host), this.port);
    this.target.once("close", this._targetCloseEventListener);
    this.target.once("error", (error) => this.socksProxy._socksProxy.sendSocketError({ uid: this.uid, error: error.message }));
    if (this._closed) {
      this.target.destroy();
      return;
    }
    this.socksProxy._socksProxy.socketConnected({
      uid: this.uid,
      host: this.target.localAddress,
      port: this.target.localPort
    });
  }
  onClose() {
    this.target.destroy();
    this.internalTLS?.destroy();
    this._dummyServer?.close();
    this._closed = true;
  }
  onData(data) {
    if (!this.firstPackageReceived) {
      this.firstPackageReceived = true;
      if (data[0] === 22)
        this._attachTLSListeners();
      else
        this.target.on("data", (data2) => this.socksProxy._socksProxy.sendSocketData({ uid: this.uid, data: data2 }));
    }
    if (this.internal)
      this.internal.push(data);
    else
      this.target.write(data);
  }
  _attachTLSListeners() {
    this.internal = new import_stream.default.Duplex({
      read: () => {
      },
      write: (data, encoding, callback) => {
        this.socksProxy._socksProxy.sendSocketData({ uid: this.uid, data });
        callback();
      }
    });
    this.socksProxy.alpnCache.get(rewriteToLocalhostIfNeeded(this.host), this.port, (alpnProtocolChosenByServer) => {
      import_debugLogger.debugLogger.log("client-certificates", `Proxy->Target ${this.host}:${this.port} chooses ALPN ${alpnProtocolChosenByServer}`);
      if (this._closed)
        return;
      this._dummyServer = import_tls.default.createServer({
        ...dummyServerTlsOptions,
        ALPNProtocols: alpnProtocolChosenByServer === "h2" ? ["h2", "http/1.1"] : ["http/1.1"]
      });
      this._dummyServer.emit("connection", this.internal);
      this._dummyServer.once("secureConnection", (internalTLS) => {
        this.internalTLS = internalTLS;
        import_debugLogger.debugLogger.log("client-certificates", `Browser->Proxy ${this.host}:${this.port} chooses ALPN ${internalTLS.alpnProtocol}`);
        let targetTLS = void 0;
        const handleError = (error) => {
          import_debugLogger.debugLogger.log("client-certificates", `error when connecting to target: ${error.message.replaceAll("\n", " ")}`);
          const responseBody = (0, import_utils.escapeHTML)("Playwright client-certificate error: " + error.message).replaceAll("\n", " <br>");
          if (internalTLS?.alpnProtocol === "h2") {
            if ("performServerHandshake" in import_http2.default) {
              this.target.removeListener("close", this._targetCloseEventListener);
              const session = import_http2.default.performServerHandshake(internalTLS);
              session.on("error", () => {
                this.target.destroy();
                this._targetCloseEventListener();
              });
              session.once("stream", (stream2) => {
                stream2.respond({
                  "content-type": "text/html",
                  [import_http2.default.constants.HTTP2_HEADER_STATUS]: 503
                });
                const cleanup = () => {
                  session.close();
                  this.target.destroy();
                  this._targetCloseEventListener();
                };
                stream2.end(responseBody, cleanup);
                stream2.once("error", cleanup);
              });
            } else {
              this.target.destroy();
            }
          } else {
            internalTLS.end([
              "HTTP/1.1 503 Internal Server Error",
              "Content-Type: text/html; charset=utf-8",
              "Content-Length: " + Buffer.byteLength(responseBody),
              "",
              responseBody
            ].join("\r\n"));
            this.target.destroy();
          }
        };
        if (this._closed) {
          internalTLS.destroy();
          return;
        }
        targetTLS = import_tls.default.connect({
          socket: this.target,
          host: this.host,
          port: this.port,
          rejectUnauthorized: !this.socksProxy.ignoreHTTPSErrors,
          ALPNProtocols: [internalTLS.alpnProtocol || "http/1.1"],
          servername: !import_net.default.isIP(this.host) ? this.host : void 0,
          secureContext: this.socksProxy.secureContextMap.get(new URL(`https://${this.host}:${this.port}`).origin)
        });
        targetTLS.once("secureConnect", () => {
          internalTLS.pipe(targetTLS);
          targetTLS.pipe(internalTLS);
        });
        internalTLS.once("error", () => this.target.destroy());
        targetTLS.once("error", handleError);
      });
    });
  }
}
class ClientCertificatesProxy {
  constructor(contextOptions) {
    this._connections = /* @__PURE__ */ new Map();
    this.secureContextMap = /* @__PURE__ */ new Map();
    (0, import_browserContext.verifyClientCertificates)(contextOptions.clientCertificates);
    this.alpnCache = new ALPNCache();
    this.ignoreHTTPSErrors = contextOptions.ignoreHTTPSErrors;
    this.proxyAgentFromOptions = (0, import_network.createProxyAgent)(contextOptions.proxy);
    this._initSecureContexts(contextOptions.clientCertificates);
    this._socksProxy = new import_socksProxy.SocksProxy();
    this._socksProxy.setPattern("*");
    this._socksProxy.addListener(import_socksProxy.SocksProxy.Events.SocksRequested, async (payload) => {
      try {
        const connection = new SocksProxyConnection(this, payload.uid, payload.host, payload.port);
        await connection.connect();
        this._connections.set(payload.uid, connection);
      } catch (error) {
        this._socksProxy.socketFailed({ uid: payload.uid, errorCode: error.code });
      }
    });
    this._socksProxy.addListener(import_socksProxy.SocksProxy.Events.SocksData, async (payload) => {
      this._connections.get(payload.uid)?.onData(payload.data);
    });
    this._socksProxy.addListener(import_socksProxy.SocksProxy.Events.SocksClosed, (payload) => {
      this._connections.get(payload.uid)?.onClose();
      this._connections.delete(payload.uid);
    });
    loadDummyServerCertsIfNeeded();
  }
  _initSecureContexts(clientCertificates) {
    const origin2certs = /* @__PURE__ */ new Map();
    for (const cert of clientCertificates || []) {
      const origin = normalizeOrigin(cert.origin);
      const certs = origin2certs.get(origin) || [];
      certs.push(cert);
      origin2certs.set(origin, certs);
    }
    for (const [origin, certs] of origin2certs) {
      try {
        this.secureContextMap.set(origin, import_tls.default.createSecureContext(convertClientCertificatesToTLSOptions(certs)));
      } catch (error) {
        error = rewriteOpenSSLErrorIfNeeded(error);
        throw (0, import_utils.rewriteErrorMessage)(error, `Failed to load client certificate: ${error.message}`);
      }
    }
  }
  static async create(contextOptions) {
    const proxy = new ClientCertificatesProxy(contextOptions);
    await proxy._socksProxy.listen(0, "127.0.0.1");
    return proxy;
  }
  proxySettings() {
    return { server: `socks5://127.0.0.1:${this._socksProxy.port()}` };
  }
  async close() {
    await this._socksProxy.close();
  }
}
function normalizeOrigin(origin) {
  try {
    return new URL(origin).origin;
  } catch (error) {
    return origin;
  }
}
function convertClientCertificatesToTLSOptions(clientCertificates) {
  if (!clientCertificates || !clientCertificates.length)
    return;
  const tlsOptions = {
    pfx: [],
    key: [],
    cert: []
  };
  for (const cert of clientCertificates) {
    if (cert.cert)
      tlsOptions.cert.push(cert.cert);
    if (cert.key)
      tlsOptions.key.push({ pem: cert.key, passphrase: cert.passphrase });
    if (cert.pfx)
      tlsOptions.pfx.push({ buf: cert.pfx, passphrase: cert.passphrase });
  }
  return tlsOptions;
}
function getMatchingTLSOptionsForOrigin(clientCertificates, origin) {
  const matchingCerts = clientCertificates?.filter(
    (c) => normalizeOrigin(c.origin) === origin
  );
  return convertClientCertificatesToTLSOptions(matchingCerts);
}
function rewriteToLocalhostIfNeeded(host) {
  return host === "local.playwright" ? "localhost" : host;
}
function rewriteOpenSSLErrorIfNeeded(error) {
  if (error.message !== "unsupported" && error.code !== "ERR_CRYPTO_UNSUPPORTED_OPERATION")
    return error;
  return (0, import_utils.rewriteErrorMessage)(error, [
    "Unsupported TLS certificate.",
    "Most likely, the security algorithm of the given certificate was deprecated by OpenSSL.",
    "For more details, see https://github.com/openssl/openssl/blob/master/README-PROVIDERS.md#the-legacy-provider",
    "You could probably modernize the certificate by following the steps at https://github.com/nodejs/node/issues/40672#issuecomment-1243648223"
  ].join("\n"));
}
// Annotate the CommonJS export names for ESM import in node:
0 && (module.exports = {
  ClientCertificatesProxy,
  getMatchingTLSOptionsForOrigin,
  rewriteOpenSSLErrorIfNeeded
});
