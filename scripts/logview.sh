#!/bin/bash

# Color definitions
RED='\033[1;31m'
GREEN='\033[1;32m'
BLUE='\033[1;34m'
YELLOW='\033[1;33m'
CYAN='\033[1;36m'
MAGENTA='\033[1;35m'
NC='\033[0m' # No Color

# Check dependencies
if ! command -v jq &>/dev/null; then
    echo -e "${RED}ERROR:${NC} 'jq' is not installed. Please install it first (e.g., with 'sudo apt install jq')."
    exit 1
fi

# Check arguments
if [ -z "$1" ]; then
    echo -e "${YELLOW}Usage:${NC} $0 <path/to/logfile.log>"
    exit 1
fi

logfile="$1"
if [ ! -f "$logfile" ]; then
    echo -e "${RED}ERROR:${NC} File '$logfile' not found."
    exit 1
fi

# Function to format the timestamp
format_time() {
    echo "$1" | sed 's/T/ /; s/\..*Z//'
}

# Function to display JSON data
show_json() {
    local json="$1"
    local indent="$2"

    echo "$json" | jq -r --arg indent "$indent" '
        to_entries[] | 
        "\($indent)\(.key): \(
            if .value|type == "object" then
                "\n\(.value | to_entries[] | "\($indent)  \(.key): \(.value)")"
            else
                .value
            end
        )"
    '
}

# Main processing function
process_entry() {
    local entry="$1"

    local level=$(echo "$entry" | jq -r '.level')
    local timestamp=$(format_time $(echo "$entry" | jq -r '.timestamp'))
    local message=$(echo "$entry" | jq -r '.message')

    case "$level" in
    "INFO") color="$GREEN" ;;
    "REQUEST") color="$BLUE" ;;
    "ERROR") color="$RED" ;;
    "WARNING") color="$YELLOW" ;;
    *) color="$MAGENTA" ;;
    esac

    echo -e "${color}╔═══════════════════════════════════════════════${NC}"
    echo -e "${color}║ [$level] $timestamp${NC}"
    echo -e "${color}╚═══════════════════════════════════════════════${NC}"

    if [[ "$message" =~ ^\{.*\}$ ]]; then
        if parsed_msg=$(echo "$message" | jq -c '.' 2>/dev/null); then
            case "$level" in
            "REQUEST")
                echo -e "${CYAN}Request Details:${NC}"
                method=$(echo "$parsed_msg" | jq -r '.method')
                url=$(echo "$parsed_msg" | jq -r '.url')
                ip=$(echo "$parsed_msg" | jq -r '.client_ip')

                # Detect IP type
                if [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
                    ip_info="${GREEN}(IPv4)${NC}"
                elif [[ "$ip" =~ : ]]; then
                    ip_info="${YELLOW}(IPv6)${NC}"
                else
                    ip_info="${RED}(Unknown)${NC}"
                fi

                echo -e "  ${CYAN}Method:${NC} $method"
                echo -e "  ${CYAN}URL:${NC} $url"
                echo -e "  ${CYAN}Client IP:${NC} $ip $ip_info"

                uploaded=$(echo "$parsed_msg" | jq '.uploaded_files')
                if [ "$uploaded" != "null" ]; then
                    echo -e "\n  ${CYAN}Uploaded Files:${NC}"
                    echo "$uploaded" | jq -c '.[]' | while read -r file; do
                        local field=$(echo "$file" | jq -r '.field')
                        local name=$(echo "$file" | jq -r '.filename')
                        local size=$(echo "$file" | jq -r '.size_bytes')
                        local ctype=$(echo "$file" | jq -r '.contenttype')
                        echo -e "        ${MAGENTA}Field:${NC} $field"
                        echo -e "        ${MAGENTA}Filename:${NC} $name"
                        echo -e "        ${MAGENTA}Size:${NC} $size Bytes"
                        echo -e "        ${MAGENTA}Type:${NC} $ctype"
                        echo ""
                    done
                fi
                ;;

            "ERROR")
                echo -e "${RED} Error details:${NC}"
                echo -e "  ${RED}Error:${NC} $(echo "$parsed_msg" | jq -r '.error')"
                echo -e "  ${RED}Status:${NC} $(echo "$parsed_msg" | jq -r '.response.status_code')"
                echo -e "  ${RED}Message:${NC} $(echo "$parsed_msg" | jq -r '.response.message')"

                echo -e "\n  ${RED} Request info:${NC}"
                echo -e "    Method: $(echo "$parsed_msg" | jq -r '.request.method // "N/A"')"
                echo -e "    Path: $(echo "$parsed_msg" | jq -r '.request.path // .request.url // "N/A"')"
                ;;

            *)
                echo -e "${YELLOW}Message content:${NC}"
                show_json "$parsed_msg" "  "
                ;;
            esac
        else
            echo -e "${color}Message:${NC} $message"
        fi
    else
        echo -e "${color}Message:${NC} $message"
    fi

    echo
}

# Process log file line by line
while IFS= read -r line; do
    [ -z "$line" ] && continue
    process_entry "$line"
done <"$logfile"
