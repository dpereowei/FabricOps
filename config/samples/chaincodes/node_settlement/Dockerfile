FROM node:18-bookworm-slim

WORKDIR /usr/src/app

COPY package*.json ./
RUN npm install --omit=dev

COPY index.js ./

EXPOSE 7052

CMD ["npm", "run", "start:ccaas"]