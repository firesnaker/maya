# Stage 1: Dependency installation and building
FROM node:20-alpine AS builder

WORKDIR /app

# Copy package.json and package-lock.json to install dependencies
COPY package.json package-lock.json ./
RUN npm install

# Copy the rest of the code
COPY . .

# Build the Next.js application
RUN npm run build

# Stage 2: Running the application
# We use the official node:20-alpine image to run the app
FROM node:20-alpine AS runner

WORKDIR /app

# Copy environment files and Next.js build output
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static/
COPY --from=builder /app/public ./public/

# Expose the port Next.js listens on (3000 by default)
EXPOSE 3000

# Start the Next.js production server
CMD ["node", "server.js"]
