#!/usr/bin/env python3

import numpy as np
import sys

"""
    File dedicating to creating a dataset to initialize the log with.
"""


USER_COUNT = 1000 #Number of distinct users to create. If None users will be
                 #created until the max leaf total is reached and the previous
                 #number of users will be used.

MAX_LEAF_COUNT = None #Maximum number of leaves generated for the dataset. If
                      #the value is None then the number of leaves will depend
                      #solely on the distribution for the user count.

LAMBDA = 10 #1 less than the Mean number of leaves per user. Number of a leaves
            #for a user is sampled from a poisson distribution with mean lambda
            #and then 1 is added so all users have at least 1 leaf.

LAMBDA_PK = 4 #Average number of public keys each user should have.

UMAX_64 = int (np.power (2.0, 64))

ID_SIZE = 4 #Number of 64 bit values in any identifiers

KEY_SIZE = 32 #Number of 64 bit values in any key


"""
    Class which holds the information about all the leaves that will be created
    for an initial tree.
"""
class Tree:

    def __init__ (self, tid):
        self.users = []
        self.id = tid
        self.counts = []
        self.generate_initial_tree ()

    """
        Returns a list of all users.
    """
    def get_users (self):
        return self.users

    """
        Returns the id of the tree.
    """
    def get_id (self):
        return self.id

    """
        Updates the public key for all users with the previous pk.
    """
    def update_pk (self, user_id, oldpk, newpk):
        user = self.find_user (user_id)
        nodes = user.get_nodes ()
        ids = []
        for node in nodes:
            if node.get_key () == oldpk:
                node.update_key (newpk)
                ids.append (node.get_id ())
        return ids

    """
        Finds the user with the given id. Returns None if the user
        does not exist.
    """
    def find_user (self, user_id):
        users = self.get_users ()
        for user in users:
            if user.get_id () == user_id:
                return user
        return None

    """
        Generates the initial tree for the user. It relies on the global
        variables to select the number of users and similarly the number
        of overall leaves to generate.
    """
    def generate_initial_tree (self):
        if USER_COUNT is None:
            if MAX_LEAF_COUNT is None:
                error_and_exit ("Cannot no user count and no leaf count")
            else:
                count = 0
                while True:
                    new_user = User ()
                    old_count = count
                    count += new_user.leaf_count ()
                    if count <= MAX_LEAF_COUNT:
                        self.users.append (new_user)
                        self.counts.append ((old_count, count))
                    else:
                        break
        elif MAX_LEAF_COUNT is None:
            count = 0
            for i in range (USER_COUNT):
                new_user = User ()
                old_count = count
                count += new_user.leaf_count ()
                self.users.append (new_user)
                self.counts.append ((old_count, count))
        else:
            count = 0
            for i in range (USER_COUNT):
                new_user = User ()
                old_count = count
                count += new_user.leaf_count ()
                if count <= MAX_LEAF_COUNT:
                    self.users.append (new_user)
                    self.counts.append ((old_count, count))
                else:
                    break

    """
        Selects a random leaf uniformly from the available leaves. Returns a
        tuple of the user, the public key, and the identifier.
    """
    def get_random_leaf (self, i, j):
        index = np.random.randint (low=self.counts[i][0], high=self.counts[j][1])
        user_num = self.count_search (index)
        user = self.users[user_num]
        leaf = user.nodes[index - self.counts[user_num][0]]
        return (user.id, leaf.key, leaf.identifier)

    """
        Prints out all the leaves in the tree as a many lines of maps from
        users to nodes.
    """
    def print_leaves (self):
        for user in self.users:
            user.print_leaves ()
            print ()

    """
        Locates the index of the user with that holds the ith leaf.
    """
    def count_search (self, i):
        low = 0
        high = len (self.counts)
        while high - low > 1:
            middle = (high + low) // 2
            count_pair = self.counts[middle]
            if count_pair[0] <= i < count_pair[1]:
                return middle
            elif count_pair[1] <= i:
                low = middle + 1
            else:
                high = middle
        return low


"""
    Class which holds the information about a user and its associated leaves.
    Each user is given an identifier for mapping purposes.
"""
class User:

    def __init__ (self):
        count = self.generate_leaf_count ()
        self.nodes = []
        self.id = random_64s (ID_SIZE)
        pk  = []
        self.pk_count = round (np.random.poisson (LAMBDA_PK)) + 1
        parition = 1.0 / self.pk_count
        for i in range (self.pk_count):
            pk.append (self.generate_key ())
        for c in range (count):
            key_select = np.random.random ()
            i = 0
            while key_select >= (i + 1) * parition:
                i += 1
            self.nodes.append (Node (pk[i]))

    """
        Returns a list of all nodes associated with the user.
    """
    def get_nodes (self):
        return self.nodes

    """
        Returns the user id
    """
    def get_id (self):
        return self.id
    
    """
        Computes the number of leaves the user maintains through sampling from
        a poisson distribution.
    """
    def generate_leaf_count (self):
        return round (np.random.poisson (LAMBDA)) + 1


    """
        Computes a key value associated with a user.
    """
    def generate_key (self):
        return random_64s (KEY_SIZE)

    """
        Returns the number of leaves the user has. Assumes that the leaf count
        has already been generated.
    """
    def leaf_count (self):
        return len (self.nodes)

    """
        Prints out the leaves associated with the user as a mapping from user
        identifier to possibly many tuples.
    """
    def print_leaves (self):
        print ("{" + str (self.id) + ": ",end="")
        for node in self.nodes:
            node.print_contents ()
        print ("}", end='')

"""
    Class which holds the base information about the leaf: a public key value
    and a unique identifier.
"""
class Node:
    def __init__ (self, key):
        self.key = key
        self.identifier = random_64s (ID_SIZE)

    """
        Prints out the contents, the public key value and identifier, 
        as a tuple.
    """
    def print_contents (self):
        print ("(" + str (self.key) + ", " + str (self.identifier) + ")", end="")

    """
        Returns the public key.
    """
    def get_key (self):
        return self.key

    """
        Returns the identifier.
    """
    def get_id (self):
        return self.identifier

    """
        Updates the pk value to the value passed in.
    """
    def update_key (self, new_key):
        self.key = new_key

"""
    Helper function to produce a random value by concatenating
    COUNT 64 bit values together.
"""
def random_64s (count):
    val = 0
    for i in range (count):
        val <<= 6
        val += int (np.random.randint (low=0, high=UMAX_64, dtype="uint64"))
    return val


"""
    Helper function to generate Errors that terminate the program and prints
    MSG to stderr.
"""
def error_and_exit (msg):
    sys.stderr.write ("Error: " + msg + "\r\n")
    sys.exit (1)

"""
    Sample main function which will generate an orignal data set and prints it.
"""
def main ():
    if len(sys.argv) != 1:
        error_and_exit ("This program does not accept any arguments")
    t = Tree(random_64s (1))
    t.print_leaves ()

if __name__ == "__main__":
    main ()
