#!/usr/bin/env python3

from datagen import error_and_exit
from enum import Enum
import numpy as np
import sys, datagen

"""
    File used to generate a series of transactions to test Trillium on.
    Each transaction refers to either a read or a write. A read leads
    to only reading from a single leaf node while a write specifies a
    user and public key pair and updates all the leaves containing that
    identifier.
"""

NUM_TRANSACTIONS = 1000 #Number of transactions to run at a given time.

WRITE_PROBABILITY = 0.2 #Percentage of transactions that should be writes.

"""
    Class used to hold the basic information for a transaction.
"""
class Transaction:

    def __init__ (self, t):
        self.tid = t.id
        self.type = TransactionType.ERROR
        self.set_transaction_type ()
        self.user, self.pk, self.id = t.get_random_leaf ()

    def print_transaction (self):
        if self.type == TransactionType.ERROR:
            print ("Error: Invalid transaction type.")
        else:
            if self.type == TransactionType.READ:
                print ("Read to: ", end="")
            else:
                print ("Write to: ", end="")
            print ("(User: {}, with Public Key: {}, and Identifier: {})".format (self.user, self.pk, self.id))

    """
        Generates a random number and decides if the transaction
        is a read or a write.
    """
    def set_transaction_type (self):
         val = np.random.random ()
         if val < WRITE_PROBABILITY:
             self.type = TransactionType.WRITE
         else:
             self.type = TransactionType.READ

"""
    Enum class used to distinguish between transactions which READ
    and transactions which WRITE.
"""
class TransactionType (Enum):
    ERROR = 0
    READ = 1
    WRITE = 2

"""
    Function used to generate a list of transactions to test concurrency on.
"""
def generate_transaction_list (t):
    return [Transaction (t) for c in range (NUM_TRANSACTIONS)]

"""
    Sample main function which will construct a sample set of transactions
    and print it.
"""
def main ():
    if len (sys.argv) != 1:
        error_and_exit ("This program does not accept any arguments.")
    t = datagen.Tree ()
    trxns = generate_transaction_list (t)
    for trxn in trxns:
        trxn.print_transaction ()

if __name__ == "__main__":
    main ()
