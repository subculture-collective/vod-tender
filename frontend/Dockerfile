# Simple Dockerfile for a Vite/React frontend
FROM node:20-alpine AS build
WORKDIR /app
COPY package.json package-lock.json* yarn.lock* ./
RUN npm install --frozen-lockfile || yarn install --frozen-lockfile
COPY . .
RUN npm run build || yarn build

FROM node:20-alpine AS serve
WORKDIR /app
COPY --from=build /app/dist ./dist
RUN npm install -g serve
EXPOSE 3000
CMD ["serve", "-s", "dist", "-l", "3000"]
