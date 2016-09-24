#---------------------------------------------------
#
# Makefile
#
#---------------------------------------------------

PACKAGES    = apng.go
BIN         = apng.exe

#---------------------------------------------------

GO          = go
SUB         = build
FLAGS       = 
all: run

run: $(BIN)
	./$(BIN)

$(BIN): $(PACKAGES) Makefile
	$(GO) $(SUB) -o $(BIN) $(FLAGS) $(PACKAGES)
