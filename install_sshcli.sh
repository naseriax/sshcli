

# Full URL to the RAW executable file on GitLab for macOS ARM64.
GITLAB_URL="https://github.com/naseriax/sshcli/releases/download/20250903.0836/sshcli_mac_arm64"

# The destination directory within the user's home folder.
DEST_DIR="$HOME/sshcli"

# The new name for the executable after it is downloaded.
NEW_NAME="sshcli"

# ==============================================================================
# SCRIPT LOGIC (DO NOT EDIT BELOW THIS LINE)
# ==============================================================================

echo "Starting download of the sshcli executable for macOS (ARM64)..."

# Create the destination directory
echo "Creating directory: $DEST_DIR"
mkdir -p "$DEST_DIR" || { echo "Error: Failed to create directory. Exiting."; exit 1; }

# The final path for the executable.
FINAL_PATH="$DEST_DIR/$NEW_NAME"

# Download the file
echo "Downloading file from: $GITLAB_URL"
curl -L --progress-bar "$GITLAB_URL" --output "$FINAL_PATH"
if [ $? -ne 0 ]; then
    echo "Error: curl download failed."
    exit 1
fi

# Give the file execute permissions
echo "Setting executable permissions..."
chmod +x "$FINAL_PATH"

echo "Download complete! The executable has been saved to:"
echo "$FINAL_PATH"

echo "Adding sshcli to PATH"
echo "# Below line was added by sshcli" >> ~/.zshrc
echo 'export PATH="$HOME/sshcli:$PATH"' >> ~/.zshrc
source ~/.zshrc

exit 0
