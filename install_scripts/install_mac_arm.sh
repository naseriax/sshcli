GITLAB_URL="https://github.com/naseriax/sshcli/releases/download/20260806.0732/sshcli_mac_arm64"
DEST_DIR="$HOME/sshcli"
NEW_NAME="sshcli"
echo "Starting download of the sshcli executable for macOS (ARM64)..."
echo "Creating directory: $DEST_DIR"
mkdir -p "$DEST_DIR" || { echo "Error: Failed to create directory. Exiting."; exit 1; }
FINAL_PATH="$DEST_DIR/$NEW_NAME"
echo "Downloading file from: $GITLAB_URL"
curl -L --progress-bar "$GITLAB_URL" --output "$FINAL_PATH"
if [ $? -ne 0 ]; then
    echo "Error: curl download failed."
    exit 1
fi
echo "Download complete! The executable has been saved to:"
echo "$FINAL_PATH"
echo "Setting executable permissions..."
chmod +x "$FINAL_PATH"
echo "Adding sshcli to PATH"

LINE_TO_ADD='export PATH="$HOME/sshcli:$PATH"'
FILE="$HOME/.zshrc"

if ! grep -Fxq "$LINE_TO_ADD" "$FILE"; then
    echo -e "\n# Below line was added by sshcli" >> "$FILE"
    echo "$LINE_TO_ADD" >> "$FILE"
fi

echo ""
echo "🎉 Success! sshcli has been installed."
echo ""
echo "To finish the setup, please do one of the following:"
echo "1) Open a new terminal window."
echo "   OR"
echo "2) Run the following command in your current terminal:"
echo "   source ~/.zshrc"
echo ""
exit 0
