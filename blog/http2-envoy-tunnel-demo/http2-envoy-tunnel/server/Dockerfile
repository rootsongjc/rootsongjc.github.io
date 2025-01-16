FROM node:14

WORKDIR /app

COPY server/server.js .

COPY certs /certs

EXPOSE 8080

CMD ["node", "server.js"]
