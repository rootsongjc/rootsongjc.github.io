const http2 = require('http2');
const fs = require('fs');

const server = http2.createSecureServer({
  key: fs.readFileSync('/certs/server.key'),
  cert: fs.readFileSync('/certs/server.crt'),
});

server.on('stream', (stream, headers) => {
  const method = headers[':method'];
  const path = headers[':path'];

  if (method === 'CONNECT') {
    console.log(`Received CONNECT request for ${path}`);

    // 响应 200，建立隧道
    stream.respond({
      ':status': 200,
    });

    // 在隧道内处理数据
    stream.on('data', (chunk) => {
      const message = chunk.toString();
      console.log(`Received from client: ${message}`);

      // 回应客户端
      const response = `Echo from server: ${message}`;
      stream.write(response);
    });

    stream.on('end', () => {
      console.log('Stream ended by client.');
      stream.end();
    });
  } else {
    // 对于非 CONNECT 请求，返回 404
    stream.respond({
      ':status': 404,
    });
    stream.end();
  }
});

server.listen(8080, () => {
  console.log('Secure HTTP/2 server is listening on port 8080');
});
