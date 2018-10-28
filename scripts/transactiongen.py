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
    Enum class used to distinguish between transactions which READ
    and transactions which WRITE.
"""
class TransactionType (Enum):
    ERROR = 0
    READ = 1
    WRITE = 2

"""
    Class used to hold the basic information for a transaction.
"""
class Transaction:

    """
        Constructor used to generate a random transaction for a tree.
    """
    def __init__ (self, t=None):
        self.type = TransactionType.ERROR
        if t is not None:
            self.tid = t.get_id ()
            self.type = TransactionType.ERROR
            self.set_transaction_type ()
            self.user, self.pk, self.id = t.get_random_leaf ()

    """
        Constructor used to generate a transaction for a particular leaf.
        If the transaction type is an error or unspecified the type of transaction
        will be random.
    """
    def configure_transaction (self, tid, user_id, pk, identifier, trx_type = TransactionType.ERROR):
        self.tid = tid
        self.user = user_id
        self.pk = pk
        self.id = identifier
        self.type = trx_type
        if trx_type == TransactionType.ERROR:
            self.set_transaction_type ()


    """
        Prints a transaction in form that can be read by a user.
    """
    def print_transaction (self):
        if self.type == TransactionType.ERROR:
            error_and_exit ("Invalid transaction type.")
        else:
            if self.type == TransactionType.READ:
                print ("Read to: ", end="")
            else:
                print ("Write to: ", end="")
            print ("(User: {}, with Public Key: {}, and Identifier: {})".format (self.user, self.pk, self.id))

    """
        Prints a transaction in a form that can be viewed as a database table.
        The table is a form of comma separated values in the form:

                       TYPE | TID | USER | PK | IDENTIFIER

        where the TYPE is 1 for read and 2 for write.
    """
    def print_transaction_as_row (self):
        if self.type == TransactionType.ERROR:
            error_and_exit ("Invalid transaction type.")
        else:
            print ("{}, {}, {}, {}, {}".format (self.type.value, self.tid, self.user, self.pk, self.id))



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
    Funciton used to generate a list of transactions for initializing the
    current tree t.
"""
def generate_init_transaction_list (t):
    trxns = []
    users = t.get_users ()
    for user in users:
        nodes = user.get_nodes ()
        for node in nodes:
            trxn = Transaction()
            trxn.configure_transaction (t.get_id (), user.get_id (), node.get_key (), node.get_id (), TransactionType.WRITE)
            trxns.append (trxn)
    return trxns

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
    tid = None
    if len (sys.argv) > 2:
        error_and_exit ("This program takes at most 1 argument, the tree id.")
    elif len (sys.argv) == 2:
        tid = int (sys.argv[1])
    else:
        tid = datagen.random_64s (4)
    t = datagen.Tree (tid)
    init_trxns = generate_init_transaction_list (t)
    trxns = generate_transaction_list (t)
    for trxn in init_trxns:
        trxn.print_transaction_as_row ()
    for trxn in trxns:
        trxn.print_transaction_as_row ()

if __name__ == "__main__":
    main ()
