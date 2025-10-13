#!/bin/bash

# WhatsApp Summarizer Bot Setup Script

echo "🤖 WhatsApp Summarizer Bot Setup"
echo "================================="

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.21 or later."
    exit 1
fi

GO_VERSION=$(go version | grep -oE '[0-9]+\.[0-9]+')
echo "✅ Go version: $GO_VERSION"

# Check if we're in the right directory
if [[ ! -f "go.mod" ]]; then
    echo "❌ go.mod not found. Please run this script from the project root."
    exit 1
fi

echo "📦 Installing dependencies..."
go mod download
go mod tidy

# Create .env file if it doesn't exist
if [[ ! -f ".env" ]]; then
    echo "📝 Creating .env file from template..."
    cp configs/.env.example .env
    echo "⚠️  Please edit .env file with your configuration:"
    echo "   - GEMINI_API_KEY=your_api_key_here"
    echo "   - OWNER_JID=your_phone_number"
    echo "   - Configure whitelists as needed"
else
    echo "✅ .env file already exists"
fi

# Create work.db file
echo "🗄️  Initializing database..."
touch work.db

# Build the application
echo "🔨 Building application..."
go build -o bin/bot ./cmd/bot
if [[ $? -eq 0 ]]; then
    echo "✅ Build successful! Binary created at: bin/bot"
else
    echo "❌ Build failed!"
    exit 1
fi

# Create run script
cat > run.sh << 'EOF'
#!/bin/bash
echo "Starting WhatsApp Summarizer Bot..."
./bin/bot
EOF
chmod +x run.sh

echo ""
echo "🎉 Setup complete!"
echo ""
echo "Next steps:"
echo "1. Edit .env file with your configuration"
echo "2. Run the bot: ./run.sh"
echo "3. Scan QR code with WhatsApp when prompted"
echo ""
echo "For help, check README_NEW.md"