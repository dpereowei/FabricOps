FROM node:18-bookworm-slim

WORKDIR /usr/src/app

COPY package*.json ./
RUN npm install --omit=dev
RUN npm ls fabric-shim fabric-contract-api @grpc/grpc-js

COPY index.js ./

EXPOSE 7052

CMD ["npm", "run", "start:ccaas"]