// BE-2 without a declared Java pkg: its types sit in the unnamed scope that
// no import can name, so this refused file hides no import (BE-8).
class Scratch {
    void broken( {}
}
