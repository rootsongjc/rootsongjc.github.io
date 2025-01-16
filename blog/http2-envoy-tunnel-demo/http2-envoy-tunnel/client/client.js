const net = require('net');

// 创建与 Envoy 的 TCP 连接
const client = net.createConnection({ port: 10000 }, () => {
  console.log('Connected to Envoy.');

  // 发送消息给服务器
  let counter = 1;
  const interval = setInterval(() => {
    const message = `Message ${counter} from client!`;
    client.write(message);
    counter += 1;
  }, 1000);

  // 关闭连接
  setTimeout(() => {
    clearInterval(interval);
    client.end();
  }, 5000);
});

client.on('data', (data) => {
  console.log(`Received from server: ${data.toString()}`);
});

client.on('end', () => {
  console.log('Disconnected from server.');
});

client.on('error', (err) => {
  console.error('Client error:', err);
});
