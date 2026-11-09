// A small Node.js HTTP server for the eBPF spike. Like a typical dev server it logs one line per request with
// console.log. Usage: node node-server.js <port>   (the port is printed as "listening <port> pid <pid>")
const http = require('http');
const port = parseInt(process.argv[2] || '0', 10);
const server = http.createServer((req, res) => {
  console.log(JSON.stringify({ level: 'info', msg: 'node request', url: req.url, ts: Date.now() }));
  res.end('ok\n');
});
server.listen(port, '127.0.0.1', () => {
  console.log(`listening ${server.address().port} pid ${process.pid}`);
});
